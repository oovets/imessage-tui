package main

import (
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/config"
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

	wsClient := ws.NewClient(cfg.ServerURL, cfg.Password)

	// Resolve the terminal background before Bubble Tea claims stdin in raw
	// mode: auto-detection queries the terminal over stdin, and done any
	// later Bubble Tea's own input reader races it for the response. Under
	// multiplexers (tmux/screen) the query is frequently answered by the
	// multiplexer itself rather than forwarded, so detection isn't
	// trustworthy there either — cfg.Theme lets it be forced explicitly.
	switch strings.ToLower(cfg.Theme) {
	case "light":
		lipgloss.SetHasDarkBackground(false)
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	default:
		lipgloss.HasDarkBackground()
	}

	p := tea.NewProgram(tui.NewAppModelWithConfig(apiClient, wsClient, cfg), tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
		os.Exit(1)
	}
}
