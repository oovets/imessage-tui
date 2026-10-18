package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// connectTimeout bounds how long startup waits for an account. Startup is
// blocking — the chat list needs the account's peers — so a dead network must
// cost this account, not hang the app.
const connectTimeout = 20 * time.Second

// Flood waits longer than this are returned as errors instead of slept
// through: a pane that silently waits a minute reads as a hung app.
const (
	maxFloodWait    = 15 * time.Second
	maxFloodRetries = 2
)

// floodWaitPadding is added to every wait: the server counts in whole seconds
// and a retry that lands just early is refused again. A variable so tests can
// run without sleeping.
var floodWaitPadding = time.Second

// ErrNotAuthorized means the stored session no longer logs in — revoked from
// another device, or expired. Only logging in again fixes it.
var ErrNotAuthorized = errors.New("telegram session is no longer authorized; run: imessage-tui telegram login")

// SessionStore keeps a session between runs. It is gotd's session.Storage
// with the "nothing stored" error left to the store, so the config package can
// keep its own and not import gotd.
type SessionStore interface {
	LoadSession(ctx context.Context) ([]byte, error)
	StoreSession(ctx context.Context, data []byte) error
}

type sessionStorage struct {
	store    SessionStore
	notFound error
}

func (s sessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	data, err := s.store.LoadSession(ctx)
	if (err != nil && errors.Is(err, s.notFound)) || (err == nil && len(data) == 0) {
		return nil, session.ErrNotFound
	}
	return data, err
}

func (s sessionStorage) StoreSession(ctx context.Context, data []byte) error {
	return s.store.StoreSession(ctx, data)
}

// Credentials are the application id and hash from my.telegram.org.
type Credentials struct {
	APIID   int
	APIHash string
}

// New connects an account and starts its realtime feed.
//
// gotd's Client.Run is the connection: it dials, reconnects and must be
// running for any call to work. So it runs for the life of the provider in its
// own goroutine, and New returns once it is up and the session is confirmed to
// still log in.
func New(account Account, creds Credentials, store SessionStore, notFound error) (*Provider, error) {
	selfID, err := strconv.ParseInt(account.ID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram account id %q is not a user id", account.ID)
	}

	dispatcher := tg.NewUpdateDispatcher()
	gaps := updates.New(updates.Config{
		Handler: dispatcher,
		OnTooLong: func() {
			// The server gave up on sending the difference; whatever was
			// missed is only in a full reload.
			log.Printf("[telegram] %s: update gap too long, resyncing", account.ID)
		},
	})

	var p *Provider
	client := telegram.NewClient(creds.APIID, creds.APIHash, telegram.Options{
		SessionStorage: sessionStorage{store: store, notFound: notFound},
		UpdateHandler:  gaps,
		Middlewares:    []telegram.Middleware{floodWait()},
		Device:         device(),
		OnConnectionState: func(state telegram.ConnectionState) {
			p.onConnectionState(state)
		},
	})
	p = newProvider(account, client.API(), selfID)
	p.updates = gaps
	p.stream = newStream(p)
	p.handle(dispatcher)

	ctx, cancel := context.WithCancel(context.Background())
	p.stop = cancel
	ready := make(chan error, 1)
	go func() {
		err := client.Run(ctx, func(ctx context.Context) error {
			status, err := client.Auth().Status(ctx)
			if err != nil {
				return fmt.Errorf("telegram auth status: %w", err)
			}
			if !status.Authorized {
				return ErrNotAuthorized
			}
			if status.User.ID != selfID {
				return fmt.Errorf("telegram session belongs to user %d, not %d", status.User.ID, selfID)
			}
			p.peers.apply([]tg.UserClass{status.User}, nil)
			ready <- nil
			// Forget: true — the UI loads everything fresh at startup, so a
			// stored update state would only replay what it is about to load.
			return gaps.Run(ctx, client.API(), selfID, updates.AuthOptions{Forget: true})
		})
		if ctx.Err() == nil {
			log.Printf("[telegram] %s: connection ended: %v", account.ID, err)
			p.setLive(false)
			p.stream.signal(p.stream.disconnected)
		}
		select {
		case ready <- err:
		default:
		}
	}()

	select {
	case err := <-ready:
		if err != nil {
			cancel()
			return nil, err
		}
	case <-time.After(connectTimeout):
		cancel()
		return nil, fmt.Errorf("telegram account %s: no connection after %s", account.ID, connectTimeout)
	}
	return p, nil
}

// onConnectionState follows gotd's connection. It is called synchronously from
// the connection's lifecycle and must not block — the stream's signals drop
// rather than wait.
func (p *Provider) onConnectionState(state telegram.ConnectionState) {
	if p == nil || p.stream == nil {
		return
	}
	switch state {
	case telegram.ConnectionStateReady:
		p.setLive(true)
		// Also on the first connection, as Slack's stream does: the UI
		// answers with a resync, right after a gap and merely redundant at
		// startup.
		p.stream.signal(p.stream.reconnected)
	case telegram.ConnectionStateDisconnected:
		p.setLive(false)
		p.stream.signal(p.stream.disconnected)
	}
}

// floodWait sleeps through short FLOOD_WAITs and retries, so a burst — a
// resync of every cached chat after a reconnect — slows down instead of
// failing chat by chat.
func floodWait() telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			for attempt := 0; ; attempt++ {
				err := next.Invoke(ctx, input, output)
				wait, ok := tgerr.AsFloodWait(err)
				if !ok || attempt >= maxFloodRetries || wait > maxFloodWait {
					return err
				}
				log.Printf("[telegram] flood wait %s on %T", wait, input)
				timer := time.NewTimer(wait + floodWaitPadding)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
	})
}

// device is what Telegram lists under Settings → Devices for this session.
func device() telegram.DeviceConfig {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return telegram.DeviceConfig{
		DeviceModel:   "imessage-tui",
		SystemVersion: runtime.GOOS + "/" + runtime.GOARCH,
		AppVersion:    version,
	}
}
