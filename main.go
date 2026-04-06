package main

import (
	"log"
	"os"

	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/tui"
	"github.com/oovets/imessage-tui/ws"
	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	// Set up file logging
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	logFile := homeDir + "/.imessage-tui.log"

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(f)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Println("========== iMessage TUI Started ==========")
	}
}

func main() {
	cfg, err := config.LoadRequired()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Connecting to %s", cfg.ServerURL)

	// Test API connectivity
	apiClient := api.NewClient(cfg.ServerURL, cfg.Password)
	if err := apiClient.Ping(); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}

	log.Println("✓ Connected to server")

	// Create WebSocket client (will try to connect during TUI init)
	wsClient := ws.NewClient(cfg.ServerURL, cfg.Password)

	// Launch TUI
	p := tea.NewProgram(tui.NewAppModel(apiClient, wsClient), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
		os.Exit(1)
	}
}
