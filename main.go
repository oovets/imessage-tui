package main

import (
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
	cfg, err := config.LoadRequired()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	apiClient := api.NewClient(cfg.ServerURL, cfg.Password)
	if err := apiClient.Ping(); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	// Link previews are fetched through the BlueBubbles server, so this is
	// iMessage's own configuration rather than the app's.
	apiClient.SetPreviewProxyURL(cfg.PreviewProxyURL)
	apiClient.SetOEmbedEndpoint(cfg.OEmbedEndpoint)

	wsClient := ws.NewClient(cfg.ServerURL, cfg.Password)

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

	// Assigned through their concrete types first: a nil *imessage.Stream put
	// straight into a []provider.Stream would be a non-nil interface holding a
	// nil pointer, which every `!= nil` guard downstream would wave through.
	backends := provider.NewRegistry(imessage.New(apiClient))
	var streams []provider.Stream
	if stream := imessage.NewStream(wsClient); stream != nil {
		streams = append(streams, stream)
	}
	streams = append(streams, connectSlack(backends)...)

	p := tea.NewProgram(
		tui.NewAppModelWithConfig(backends, provider.Merge(streams...), cfg),
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
		log.Fatalf("Error running program: %v", err)
		os.Exit(1)
	}
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
