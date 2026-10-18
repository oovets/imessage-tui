package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/provider"
	"github.com/oovets/imessage-tui/provider/imessage"
	slackprovider "github.com/oovets/imessage-tui/provider/slack"
	telegramprovider "github.com/oovets/imessage-tui/provider/telegram"
	"github.com/oovets/imessage-tui/tui"
	"github.com/oovets/imessage-tui/ws"
)

func init() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	logFile := homeDir + "/.imessage-tui.log"

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err == nil {
		log.SetOutput(f)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
}

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCommand(os.Args[1:]))
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("reading config: %v", err)
	}

	imessageBackend, imessageStream, imessageErr := connectIMessage(cfg)

	// Resolve the terminal background before Bubble Tea claims stdin in raw
	// mode: auto-detection queries the terminal over stdin, and done any
	// later Bubble Tea's own input reader races it for the response. Under
	// multiplexers (tmux/screen) the query is frequently answered by the
	// multiplexer itself rather than forwarded, so detection isn't
	// trustworthy there either — cfg.Theme lets it be forced explicitly.
	//
	// A pinned theme also tunes the palette (tui.ApplyTheme): once the
	// background is a given rather than a guess, colors that otherwise have to
	// stay readable on white *and* black can be brightened for the one that is
	// actually there. Auto deliberately does not — a wrong guess would paint
	// unreadable text, which is worse than a merely conservative palette.
	switch strings.ToLower(cfg.Theme) {
	case "light":
		lipgloss.SetHasDarkBackground(false)
		tui.ApplyTheme(false)
	case "dark":
		lipgloss.SetHasDarkBackground(true)
		tui.ApplyTheme(true)
	default:
		lipgloss.HasDarkBackground()
	}

	backends := provider.NewRegistry(imessageBackend)
	var streams []provider.Stream
	if imessageStream != nil {
		streams = append(streams, imessageStream)
	}
	streams = append(streams, connectSlack(backends)...)
	telegramStreams, telegramErrs := connectTelegram(backends)
	streams = append(streams, telegramStreams...)

	startupErrs := append([]error{imessageErr}, telegramErrs...)
	if len(backends.All()) == 0 {
		if err := joinErrors(startupErrs); err != nil {
			fatal("%v", err)
		}
		fatal("no network is configured: set BB_SERVER_URL and BB_PASSWORD for imessage, add a slack token, or run: imessage-tui telegram login (see README)")
	}

	app := tui.NewAppModelWithConfig(backends, provider.Merge(streams...), cfg)
	if err := joinErrors(startupErrs); err != nil {
		// The other networks are up, so one being down is something to say,
		// not a reason to keep them from starting.
		log.Printf("starting without: %v", err)
		app.ShowError(err)
	}

	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		// Cell motion, not all motion: the app only reads motion while a
		// button is held (dragging the sidebar edge and the pane dividers),
		// and nothing reacts to hover. All-motion reports every movement
		// across the terminal as an escape sequence, which floods the input
		// reader for no gain — and a flooded reader is what tears sequences
		// in half and types them into the composer.
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fatal("running the tui: %v", err)
	}
}

// fatal reports a startup failure on stderr as well as in the log. Bubble Tea
// has not claimed the terminal yet (or has given it back), and with the log
// alone a missing setting looked like the program doing nothing at all.
func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	fmt.Fprintln(os.Stderr, "imessage-tui: "+msg)
	os.Exit(1)
}

// joinErrors folds startup problems into one status-line message. nil entries
// are skipped; errors.Join would put each on its own line, and the status line
// has one.
func joinErrors(errs []error) error {
	var parts []string
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}

// connectIMessage sets up the BlueBubbles backend when it is configured.
//
// Not configured is not an error: iMessage is one network among several, and
// someone may run this for Slack alone. Configured but unreachable is returned
// as err beside a nil backend — the chat list is loaded once at startup, so a
// server that is down now would contribute nothing but a 15-second timeout to
// every other network's chats.
//
// Both returns are untyped nil when there is no backend. A nil *imessage.Stream
// in a provider.Stream would be a non-nil interface holding a nil pointer,
// which every `!= nil` guard downstream would wave through.
func connectIMessage(cfg *config.Config) (provider.Provider, provider.Stream, error) {
	if !cfg.HasCredentials() {
		return nil, nil, nil
	}

	apiClient := api.NewClient(cfg.ServerURL, cfg.Password)
	if err := apiClient.Ping(); err != nil {
		return nil, nil, fmt.Errorf("imessage: bluebubbles server unreachable: %w", err)
	}
	// Link previews are fetched through the BlueBubbles server, so this is
	// iMessage's own configuration rather than the app's.
	apiClient.SetPreviewProxyURL(cfg.PreviewProxyURL)
	apiClient.SetOEmbedEndpoint(cfg.OEmbedEndpoint)

	stream := imessage.NewStream(ws.NewClient(cfg.ServerURL, cfg.Password))
	if stream == nil {
		return imessage.New(apiClient), nil, nil
	}
	return imessage.New(apiClient), stream, nil
}

// connectSlack registers every configured Slack workspace and returns their
// realtime streams.
//
// Deliberately non-fatal, the way the desktop client is: with no tokens the
// app runs exactly as it did before, and a workspace whose token has expired
// costs that workspace only. Slack failing must never keep iMessage from
// starting.
func connectSlack(backends *provider.Registry) []provider.Stream {
	workspaces, imported, err := config.LoadSlackWorkspaces()
	if err != nil {
		log.Printf("slack: reading credentials failed: %v", err)
		return nil
	}
	if imported {
		log.Printf("slack: imported %d workspace(s) from %s into the keyring; that file can now be deleted",
			len(workspaces), config.LegacySlackConfigPath())
	}

	// With one workspace the name in front of every chat is noise; with two it
	// is the only thing separating two conversations called "Anna".
	labelWorkspaces := len(workspaces) > 1

	var streams []provider.Stream
	for _, workspace := range workspaces {
		backend, err := slackprovider.New(slackprovider.Workspace{
			ID:       workspace.ID,
			Name:     workspace.Name,
			Token:    workspace.Token,
			AppToken: workspace.AppToken,
		})
		if err != nil {
			// Never log the error's own text without context, and never the
			// tokens: the message carries the workspace name only.
			log.Printf("slack: workspace %q unavailable: %v", workspace.Name, err)
			continue
		}
		backend.ShowWorkspaceInNames(labelWorkspaces)
		backends.Register(backend.GUIDPrefix(), backend)
		log.Printf("slack: connected workspace %q as %s", workspace.Name, backend.SelfUserID())

		if stream := slackprovider.NewStream(backend); stream != nil {
			streams = append(streams, stream)
		} else {
			log.Printf("slack: workspace %q has no app-level token, realtime is off (polling only)", workspace.Name)
		}
	}
	return streams
}

// connectTelegram registers every logged-in Telegram account and returns their
// realtime streams.
//
// An account that cannot connect is reported, not fatal — a revoked session
// costs that account, never the app — and its error says what fixes it.
func connectTelegram(backends *provider.Registry) ([]provider.Stream, []error) {
	accounts, err := config.LoadTelegramAccounts()
	if err != nil {
		log.Printf("telegram: reading accounts failed: %v", err)
		return nil, []error{fmt.Errorf("telegram: reading accounts from the keyring failed: %w", err)}
	}

	// As with Slack workspaces: one account needs no label, two need one
	// each to tell their chats apart.
	labelAccounts := len(accounts) > 1

	var (
		streams []provider.Stream
		errs    []error
	)
	for _, account := range accounts {
		backend, err := telegramprovider.New(
			telegramprovider.Account{ID: account.ID, Name: account.Name},
			telegramprovider.Credentials{APIID: account.APIID, APIHash: account.APIHash},
			config.TelegramSessionStore{AccountID: account.ID},
			config.ErrTelegramSessionNotFound,
		)
		if err != nil {
			log.Printf("telegram: account %s unavailable: %v", account.ID, err)
			errs = append(errs, fmt.Errorf("telegram %s: %w", account.Name, err))
			continue
		}
		backend.ShowAccountInNames(labelAccounts)
		backends.Register(backend.GUIDPrefix(), backend)
		log.Printf("telegram: connected account %s (%s)", account.Name, account.ID)
		if stream := telegramprovider.NewStream(backend); stream != nil {
			streams = append(streams, stream)
		}
	}
	return streams, errs
}
