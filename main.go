package main

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
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

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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

	p := tea.NewProgram(tui.NewAppModelWithConfig(apiClient, wsClient, cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
		os.Exit(1)
	}
}
