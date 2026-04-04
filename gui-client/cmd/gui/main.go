package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/oovets/bluebubbles-gui/gui"
	"github.com/oovets/bluebubbles-gui/api"
	"github.com/oovets/bluebubbles-gui/config"
	"github.com/oovets/bluebubbles-gui/ws"
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
	if !cfg.HasCredentials() {
		serverURL, password, err := runFirstRunWizard(cfg.ServerURL)
		if err != nil {
			log.Fatalf("First-run setup was not completed: %v", err)
		}
		if err := config.SaveCredentials(serverURL, password); err != nil {
			log.Fatalf("Failed to save credentials: %v", err)
		}
		cfg, err = config.Load()
		if err != nil {
			log.Fatalf("Failed to reload config after setup: %v", err)
		}
	}
	if !cfg.HasCredentials() {
		log.Fatalf("Missing credentials after setup. Set BB_SERVER_URL/BB_PASSWORD or rerun GUI setup")
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

func runFirstRunWizard(initialServerURL string) (string, string, error) {
	setupApp := app.NewWithID("com.bluebubbles-tui.gui.setup")
	win := setupApp.NewWindow("BlueBubbles - First-time setup")
	win.Resize(fyne.NewSize(580, 360))

	serverEntry := widget.NewEntry()
	serverEntry.SetPlaceHolder("https://your-server:1234")
	serverEntry.SetText(strings.TrimSpace(initialServerURL))

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("BlueBubbles API password")

	statusLabel := widget.NewLabel("Fyll i server och lösenord. Testa anslutning innan du sparar.")
	statusLabel.Wrapping = fyne.TextWrapWord

	showPassword := widget.NewCheck("Visa lösenord", func(checked bool) {
		passwordEntry.Password = !checked
		passwordEntry.Refresh()
	})

	var (
		resultURL      string
		resultPassword string
		resultErr      = errors.New("setup cancelled")
		finished       bool
		closing        bool
	)

	var testBtn *widget.Button
	var saveBtn *widget.Button
	setBusy := func(busy bool) {
		fyne.Do(func() {
			if busy {
				testBtn.Disable()
				saveBtn.Disable()
				serverEntry.Disable()
				passwordEntry.Disable()
				showPassword.Disable()
				return
			}
			testBtn.Enable()
			saveBtn.Enable()
			serverEntry.Enable()
			passwordEntry.Enable()
			showPassword.Enable()
		})
	}

	setStatus := func(text string) {
		fyne.Do(func() {
			statusLabel.SetText(text)
		})
	}

	runPing := func(serverURL, password string) error {
		if err := validateServerURL(serverURL); err != nil {
			return err
		}
		if strings.TrimSpace(password) == "" {
			return fmt.Errorf("lösenord saknas")
		}
		client := api.NewClient(strings.TrimSpace(serverURL), strings.TrimSpace(password))
		if err := client.Ping(); err != nil {
			return fmt.Errorf("kunde inte ansluta: %w", err)
		}
		return nil
	}

	testBtn = widget.NewButton("Testa anslutning", func() {
		serverURL := strings.TrimSpace(serverEntry.Text)
		password := strings.TrimSpace(passwordEntry.Text)
		setBusy(true)
		setStatus("Testar anslutning...")
		go func() {
			if err := runPing(serverURL, password); err != nil {
				setBusy(false)
				setStatus(err.Error())
				return
			}
			setBusy(false)
			setStatus("Anslutning OK")
		}()
	})

	saveBtn = widget.NewButton("Spara och fortsätt", func() {
		serverURL := strings.TrimSpace(serverEntry.Text)
		password := strings.TrimSpace(passwordEntry.Text)
		setBusy(true)
		setStatus("Verifierar anslutning...")
		go func() {
			if err := runPing(serverURL, password); err != nil {
				setBusy(false)
				setStatus(err.Error())
				return
			}
			resultURL = serverURL
			resultPassword = password
			resultErr = nil
			finished = true
			setStatus("Klart. Startar appen...")
			fyne.Do(func() {
				win.Close()
			})
		}()
	})

	quitBtn := widget.NewButton("Avbryt", func() {
		finished = true
		resultErr = errors.New("setup cancelled by user")
		win.Close()
	})

	win.SetCloseIntercept(func() {
		if closing {
			return
		}
		closing = true
		if !finished {
			resultErr = errors.New("setup window closed before completion")
		}
		win.Close()
	})

	content := container.NewVBox(
		widget.NewRichTextFromMarkdown("## Välkommen till BlueBubbles GUI"),
		widget.NewLabel("Ange server och lösenord en gång. Vi sparar dem för nästa start."),
		widget.NewForm(
			widget.NewFormItem("Server URL", serverEntry),
			widget.NewFormItem("Password", passwordEntry),
		),
		showPassword,
		statusLabel,
		container.NewHBox(testBtn, saveBtn, quitBtn),
	)

	win.SetContent(container.NewPadded(content))
	win.Show()
	setupApp.Run()

	if resultErr != nil {
		return "", "", resultErr
	}
	return resultURL, resultPassword, nil
}

func validateServerURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("server URL saknas")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("ogiltig URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server URL måste inkludera schema och host, t.ex. https://host:1234")
	}
	return nil
}
