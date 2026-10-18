package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Prompter is the terminal a login talks to. Logging in happens in a
// subcommand before the TUI starts, so it may print and read freely.
type Prompter interface {
	// ShowQR draws a login URL for the phone to scan. It is called again
	// with a fresh URL each time the previous one expires.
	ShowQR(url string) error
	Phone() (string, error)
	// Code asks for the login code Telegram sent to the app or by SMS.
	Code() (string, error)
	// Password asks for the two-step verification password, without echo.
	Password() (string, error)
}

// LoginResult is a logged-in account and the session that keeps it so.
type LoginResult struct {
	Account Account
	// Session is the secret to store: whoever has it is logged in.
	Session []byte
}

// Login signs an account in, by QR code — scanned in Telegram under
// Settings → Devices → Link Desktop Device — or with usePhone, by a code sent
// to the phone number. Either way a two-step password is asked for when the
// account has one.
//
// The session is built in memory and only handed back once the login has
// succeeded, so a login abandoned halfway leaves nothing in the keyring.
func Login(ctx context.Context, creds Credentials, usePhone bool, prompt Prompter) (LoginResult, error) {
	storage := &session.StorageMemory{}
	dispatcher := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(dispatcher)
	client := telegram.NewClient(creds.APIID, creds.APIHash, telegram.Options{
		SessionStorage: storage,
		UpdateHandler:  dispatcher,
		Device:         device(),
	})

	var self *tg.User
	err := client.Run(ctx, func(ctx context.Context) error {
		if usePhone {
			flow := auth.NewFlow(phoneAuth{prompt: prompt}, auth.SendCodeOptions{})
			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return err
			}
		} else if err := qrLogin(ctx, client, loggedIn, prompt); err != nil {
			return err
		}
		user, err := client.Self(ctx)
		self = user
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}

	data, err := storage.Bytes(nil)
	if err != nil {
		return LoginResult{}, fmt.Errorf("reading the new session: %w", err)
	}
	return LoginResult{
		Account: Account{ID: strconv.FormatInt(self.ID, 10), Name: userName(self)},
		Session: data,
	}, nil
}

func qrLogin(ctx context.Context, client *telegram.Client, loggedIn qrlogin.LoggedIn, prompt Prompter) error {
	_, err := client.QR().Auth(ctx, loggedIn, func(_ context.Context, token qrlogin.Token) error {
		return prompt.ShowQR(token.URL())
	})
	if err == nil {
		return nil
	}
	if !tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		return err
	}
	password, err := prompt.Password()
	if err != nil {
		return err
	}
	if _, err := client.Auth().Password(ctx, password); err != nil {
		if errors.Is(err, auth.ErrPasswordInvalid) {
			return errors.New("wrong two-step verification password")
		}
		return err
	}
	return nil
}

// Logout ends a session on Telegram's side, so it disappears from the
// account's device list rather than lingering there after the keyring entry
// is gone.
func Logout(ctx context.Context, creds Credentials, store SessionStore, notFound error) error {
	client := telegram.NewClient(creds.APIID, creds.APIHash, telegram.Options{
		SessionStorage: sessionStorage{store: store, notFound: notFound},
		NoUpdates:      true,
		Device:         device(),
	})
	return client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().AuthLogOut(ctx)
		return err
	})
}

// phoneAuth is gotd's login conversation over a Prompter. Signing up a new
// account is deliberately not offered: a terminal chat client is not where an
// account should be created by accident.
type phoneAuth struct {
	prompt Prompter
}

func (a phoneAuth) Phone(context.Context) (string, error) { return a.prompt.Phone() }

func (a phoneAuth) Password(context.Context) (string, error) { return a.prompt.Password() }

func (a phoneAuth) Code(context.Context, *tg.AuthSentCode) (string, error) {
	code, err := a.prompt.Code()
	return strings.TrimSpace(code), err
}

func (a phoneAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return &auth.SignUpRequired{TermsOfService: tos}
}

func (a phoneAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("this number has no Telegram account; create one in the Telegram app first")
}
