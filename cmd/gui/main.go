package main

import (
	"flag"
	"io"
	"log"
	"os"

	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/config"
	"github.com/bluebubbles-tui/gui"
	"github.com/bluebubbles-tui/ws"
)

func init() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	logFile := homeDir + "/.bluebubbles-gui.log"
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	var w io.Writer = os.Stdout
	if err == nil {
		w = io.MultiWriter(os.Stdout, f)
	}
	log.SetOutput(w)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("========== BlueBubbles GUI Started ==========")
}

func main() {
	chatGUID := flag.String("chat-guid", "", "open this chat GUID in the focused pane on startup")
	detachedPane := flag.Bool("detached-pane", false, "launch in detached pane mode (no contact list, no split restore)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Connecting to %s", cfg.ServerURL)

	apiClient := api.NewClient(cfg.ServerURL, cfg.Password)
	apiClient.SetPreviewProxyURL(cfg.PreviewProxyURL)
	apiClient.SetOEmbedEndpoint(cfg.OEmbedEndpoint)
	if err := apiClient.Ping(); err != nil {
		log.Fatalf("Failed to connect to BlueBubbles server: %v", err)
	}

	log.Println("Connected to BlueBubbles server")

	wsClient := ws.NewClient(cfg.ServerURL, cfg.Password)

	gui.NewApp(apiClient, wsClient, cfg, *chatGUID, *detachedPane).Run()
}
