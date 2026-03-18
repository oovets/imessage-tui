package gui

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/models"
	"github.com/bluebubbles-tui/ws"
)

// App is the top-level GUI application.
type App struct {
	fyneApp  fyne.App
	win      fyne.Window
	appTheme *compactTheme

	apiClient *api.Client
	wsClient  *ws.Client

	chatListComp  *ChatList
	paneManager   *PaneManager
	split         *container.Split
	topBarHolder  *fyne.Container
	contentHolder *fyne.Container
	showChatList  bool
	showTopBar    bool

	mu       sync.Mutex
	msgCache map[string][]models.Message
}

// NewApp creates a new GUI application using the given API and WebSocket clients.
func NewApp(apiClient *api.Client, wsClient *ws.Client) *App {
	return &App{
		apiClient: apiClient,
		wsClient:  wsClient,
		msgCache:  make(map[string][]models.Message),
	}
}

// Run builds the window and blocks until the window is closed.
func (a *App) Run() {
	a.fyneApp = app.New()
	a.appTheme = newCompactTheme()
	a.fyneApp.Settings().SetTheme(a.appTheme)

	a.win = a.fyneApp.NewWindow("BlueBubbles")
	a.win.Resize(fyne.NewSize(960, 640))

	a.chatListComp = NewChatList(func(chat *models.Chat) {
		a.selectChat(chat)
	})

	a.paneManager = NewPaneManager(
		func(pane *ChatPane, text string, replyTo *models.Message) { a.sendMessageFromPane(pane, text, replyTo) },
		func(pane *ChatPane) { /* focus tracked inside PaneManager */ },
		a.handleInputShortcut,
	)

	a.split = container.NewHSplit(a.chatListComp.Widget(), a.paneManager.Widget())
	a.split.SetOffset(0.25)
	a.showChatList = true
	a.showTopBar = true
	a.topBarHolder = container.NewMax()
	a.contentHolder = container.NewMax(a.split)
	a.refreshTopBar()
	a.win.SetContent(container.NewBorder(a.topBarHolder, nil, nil, nil, a.contentHolder))

	// Keyboard shortcuts ─────────────────────────────────────────────────
	// Ctrl+H  split focused pane side by side (horizontal)
	// Ctrl+J  split focused pane top/bottom   (vertical)
	// Ctrl+W  close focused pane
	c := a.win.Canvas()
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("H"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.splitFocusedHorizontal()
	})
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("J"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.splitFocusedVertical()
	})
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("W"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		// GLFW may emit Ctrl+W while typing in Entry before normal shortcut handling.
		// Ignore close-pane in that state to avoid accidental pane closes.
		if a.paneManager.IsFocusedInputActive() {
			return
		}
		a.closeFocusedPane()
	})
	// Ctrl+S  toggle chat list visibility
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("S"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.toggleChatListVisibility()
	})
	// Ctrl+M  toggle top menu bar visibility
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("M"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.setTopBarVisible(!a.showTopBar)
	})

	go a.loadChats()
	go a.runWebSocket()

	a.win.ShowAndRun()
}

func (a *App) splitFocusedHorizontal() {
	a.paneManager.SplitFocused(splitHorizontal)
	a.scrollAllPanes()
}

func (a *App) splitFocusedVertical() {
	a.paneManager.SplitFocused(splitVertical)
	a.scrollAllPanes()
}

func (a *App) closeFocusedPane() {
	a.paneManager.CloseFocused()
	a.scrollAllPanes()
}

func (a *App) toggleChatListVisibility() {
	a.showChatList = !a.showChatList
	if a.showChatList {
		a.contentHolder.Objects = []fyne.CanvasObject{a.split}
	} else {
		a.contentHolder.Objects = []fyne.CanvasObject{a.paneManager.Widget()}
	}
	a.contentHolder.Refresh()
}

func (a *App) setTopBarVisible(visible bool) {
	a.showTopBar = visible
	a.refreshTopBar()
}

func (a *App) refreshTopBar() {
	if a.showTopBar {
		a.topBarHolder.Objects = []fyne.CanvasObject{a.buildToolbar()}
	} else {
		a.topBarHolder.Objects = []fyne.CanvasObject{a.buildShowToolbarButton()}
	}
	a.topBarHolder.Refresh()
}

func (a *App) handleInputShortcut(shortcut fyne.Shortcut) bool {
	custom, ok := shortcut.(*desktop.CustomShortcut)
	if !ok {
		return false
	}
	if custom.Modifier&fyne.KeyModifierControl == 0 {
		return false
	}

	key := fyne.KeyName(strings.ToUpper(string(custom.KeyName)))
	switch key {
	case fyne.KeyName("H"):
		a.splitFocusedHorizontal()
		return true
	case fyne.KeyName("J"):
		a.splitFocusedVertical()
		return true
	case fyne.KeyName("S"):
		a.toggleChatListVisibility()
		return true
	case fyne.KeyName("M"):
		a.setTopBarVisible(!a.showTopBar)
		return true
	default:
		return false
	}
}

// selectChat loads the given chat into the focused pane.
// Called on the Fyne main goroutine (from chat list OnSelected).
func (a *App) selectChat(chat *models.Chat) {
	pane := a.paneManager.FocusedPane()
	if pane == nil {
		return
	}

	chatGUID := chat.GUID
	pane.ChatGUID = chatGUID
	pane.ClearReplyTarget()

	a.chatListComp.ClearNewMessage(chatGUID)
	pane.msgView.SetChatName(chat.GetDisplayName())
	pane.msgView.SetMessages(nil)

	go a.loadMessagesForPane(pane, chatGUID)
}

// loadMessagesForPane fetches messages and updates the given pane.
func (a *App) loadMessagesForPane(pane *ChatPane, chatGUID string) {
	msgs, err := a.apiClient.GetMessages(chatGUID, 50)
	if err != nil {
		log.Printf("[GUI] GetMessages error: %v", err)
		return
	}

	// Merge with any WS messages that arrived after the API snapshot.
	a.mu.Lock()
	if len(msgs) > 0 {
		newest := msgs[len(msgs)-1].DateCreated
		for _, cm := range a.msgCache[chatGUID] {
			if cm.DateCreated <= newest {
				continue
			}
			found := false
			for _, m := range msgs {
				if m.GUID == cm.GUID {
					found = true
					break
				}
			}
			if !found {
				msgs = append(msgs, cm)
			}
		}
	}
	a.msgCache[chatGUID] = msgs
	a.mu.Unlock()

	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].DateCreated < msgs[j].DateCreated
	})

	fyne.Do(func() {
		// Only update if this pane still shows the same chat.
		if pane.ChatGUID == chatGUID {
			pane.msgView.SetMessages(msgs)
		}
	})
}

// sendMessageFromPane sends a message on behalf of the given pane.
func (a *App) sendMessageFromPane(pane *ChatPane, text string, replyTo *models.Message) {
	chatGUID := pane.ChatGUID
	if chatGUID == "" {
		return
	}

	payload := text
	if replyTo != nil {
		sender := messageSenderName(*replyTo)
		quoted := strings.ReplaceAll(strings.TrimSpace(replyTo.Text), "\n", " ")
		quoted = truncateString(quoted, 140)
		if quoted == "" {
			quoted = "(message without text)"
		}
		payload = fmt.Sprintf("> %s: %s\n%s", sender, quoted, text)
	}

	go func() {
		if err := a.apiClient.SendMessage(chatGUID, payload); err != nil {
			log.Printf("[GUI] SendMessage error: %v", err)
			return
		}

		msgs, err := a.apiClient.GetMessages(chatGUID, 50)
		if err != nil {
			log.Printf("[GUI] GetMessages after send error: %v", err)
			return
		}

		a.mu.Lock()
		a.msgCache[chatGUID] = msgs
		a.mu.Unlock()

		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].DateCreated < msgs[j].DateCreated
		})

		fyne.Do(func() {
			if pane.ChatGUID == chatGUID {
				pane.msgView.SetMessages(msgs)
			}
		})
	}()
}

// loadChats fetches the chat list and pre-selects the first entry.
func (a *App) loadChats() {
	chats, err := a.apiClient.GetChats(50)
	if err != nil {
		log.Printf("[GUI] GetChats error: %v", err)
		return
	}

	fyne.Do(func() {
		a.chatListComp.SetChats(chats)
		if len(chats) > 0 {
			first := chats[0]
			a.selectChat(&first)
		}
	})
}

// runWebSocket connects the WebSocket client and processes incoming events.
func (a *App) runWebSocket() {
	if err := a.wsClient.Connect(); err != nil {
		log.Printf("[GUI] WebSocket connect failed: %v", err)
		return
	}
	log.Println("[GUI] WebSocket connected")

	for event := range a.wsClient.Events {
		a.handleWSEvent(event)
	}
	log.Println("[GUI] WebSocket closed")
}

// scrollAllPanes re-scrolls every pane to the bottom after a layout rebuild.
func (a *App) scrollAllPanes() {
	for _, p := range a.paneManager.AllPanes() {
		p.msgView.ScrollToBottom()
	}
}

// buildToolbar builds the top toolbar.
func (a *App) buildToolbar() fyne.CanvasObject {
	smaller := widget.NewButton("A-", func() {
		if a.appTheme.fontSize > 8 {
			a.appTheme.fontSize--
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}
	})
	larger := widget.NewButton("A+", func() {
		if a.appTheme.fontSize < 20 {
			a.appTheme.fontSize++
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}
	})

	var boldBtn *widget.Button
	boldBtn = widget.NewButton("B", func() {
		a.appTheme.boldAll = !a.appTheme.boldAll
		if a.appTheme.boldAll {
			boldBtn.Importance = widget.HighImportance
		} else {
			boldBtn.Importance = widget.MediumImportance
		}
		boldBtn.Refresh()
		a.fyneApp.Settings().SetTheme(a.appTheme)
	})
	boldBtn.Importance = widget.MediumImportance

	families := a.appTheme.availableFamilies()
	fontSelect := widget.NewSelect(families, func(name string) {
		a.appTheme.curFamily = name
		a.fyneApp.Settings().SetTheme(a.appTheme)
	})
	fontSelect.Selected = a.appTheme.curFamily

	var modeBtn *widget.Button
	modeBtnLabel := func() string {
		if a.appTheme.dark {
			return "Light"
		}
		return "Dark"
	}
	modeBtn = widget.NewButton(modeBtnLabel(), func() {
		a.appTheme.dark = !a.appTheme.dark
		modeBtn.SetText(modeBtnLabel())
		a.fyneApp.Settings().SetTheme(a.appTheme)
	})

	hideBtn := widget.NewButton("X", func() {
		a.setTopBarVisible(false)
	})

	return container.NewHBox(
		smaller,
		larger,
		boldBtn,
		widget.NewSeparator(),
		fontSelect,
		widget.NewSeparator(),
		modeBtn,
		layout.NewSpacer(),
		hideBtn,
	)
}

func (a *App) buildShowToolbarButton() fyne.CanvasObject {
	showBtn := widget.NewButton("Menu", func() {
		a.setTopBarVisible(true)
	})
	return container.NewHBox(showBtn, layout.NewSpacer())
}

// handleWSEvent processes a single WebSocket event. Called from the WS goroutine.
func (a *App) handleWSEvent(event models.WSEvent) {
	switch event.Type {
	case "new-message":
		var wsMsg struct {
			models.Message
			Chats []struct {
				GUID string `json:"guid"`
			} `json:"chats"`
		}
		if err := json.Unmarshal(event.Data, &wsMsg); err != nil {
			return
		}

		msg := wsMsg.Message
		if len(wsMsg.Chats) > 0 {
			msg.ChatGUID = wsMsg.Chats[0].GUID
		}
		if msg.ChatGUID == "" {
			return
		}

		a.mu.Lock()
		cached := a.msgCache[msg.ChatGUID]
		alreadyCached := false
		for _, m := range cached {
			if m.GUID == msg.GUID {
				alreadyCached = true
				break
			}
		}
		if !alreadyCached {
			a.msgCache[msg.ChatGUID] = append(cached, msg)
		}
		a.mu.Unlock()

		fyne.Do(func() {
			panesShowing := a.paneManager.PanesShowingChat(msg.ChatGUID)
			if len(panesShowing) > 0 {
				for _, p := range panesShowing {
					p.msgView.AppendMessage(msg)
				}
			} else {
				a.chatListComp.MarkNewMessage(msg.ChatGUID)
			}
		})
	}
}
