package gui

import (
	"encoding/json"
	"log"
	"sort"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/models"
	"github.com/bluebubbles-tui/ws"
)

// App is the top-level GUI application.
type App struct {
	fyneApp   fyne.App
	win       fyne.Window
	appTheme  *compactTheme

	apiClient *api.Client
	wsClient  *ws.Client

	chatListComp  *ChatList
	paneManager   *PaneManager
	split         *container.Split
	contentHolder *fyne.Container
	showChatList  bool

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
		func(pane *ChatPane, text string) { a.sendMessageFromPane(pane, text) },
		func(pane *ChatPane) { /* focus tracked inside PaneManager */ },
	)

	a.split = container.NewHSplit(a.chatListComp.Widget(), a.paneManager.Widget())
	a.split.SetOffset(0.25)
	a.showChatList = true
	a.contentHolder = container.NewMax(a.split)
	a.win.SetMainMenu(a.buildMenu())
	a.win.SetContent(a.contentHolder)

	// Keyboard shortcuts ─────────────────────────────────────────────────
	// Ctrl+H  split focused pane side by side (horizontal)
	// Ctrl+J  split focused pane top/bottom   (vertical)
	// Ctrl+W  close focused pane
	c := a.win.Canvas()
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("H"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.paneManager.SplitFocused(splitHorizontal)
		a.scrollAllPanes()
	})
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("J"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.paneManager.SplitFocused(splitVertical)
		a.scrollAllPanes()
	})
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("W"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.paneManager.CloseFocused()
		a.scrollAllPanes()
	})
	// Ctrl+S  toggle chat list visibility
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("S"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.showChatList = !a.showChatList
		if a.showChatList {
			a.contentHolder.Objects = []fyne.CanvasObject{a.split}
		} else {
			a.contentHolder.Objects = []fyne.CanvasObject{a.paneManager.Widget()}
		}
		a.contentHolder.Refresh()
	})

	go a.loadChats()
	go a.runWebSocket()

	a.win.ShowAndRun()
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
func (a *App) sendMessageFromPane(pane *ChatPane, text string) {
	chatGUID := pane.ChatGUID
	if chatGUID == "" {
		return
	}

	go func() {
		if err := a.apiClient.SendMessage(chatGUID, text); err != nil {
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

// buildMenu builds the window menu bar with font size, font family, and dark/light controls.
func (a *App) buildMenu() *fyne.MainMenu {
	larger := fyne.NewMenuItem("A+  Larger", func() {
		if a.appTheme.fontSize < 20 {
			a.appTheme.fontSize++
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}
	})
	smaller := fyne.NewMenuItem("A-  Smaller", func() {
		if a.appTheme.fontSize > 8 {
			a.appTheme.fontSize--
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}
	})

	var fontMenuItems []*fyne.MenuItem
	for _, name := range a.appTheme.availableFamilies() {
		n := name
		fontMenuItems = append(fontMenuItems, fyne.NewMenuItem(n, func() {
			a.appTheme.curFamily = n
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}))
	}
	fontItem := fyne.NewMenuItem("Font", nil)
	fontItem.ChildMenu = fyne.NewMenu("", fontMenuItems...)

	var boldItem *fyne.MenuItem
	boldItem = fyne.NewMenuItem("Bold: Off", func() {
		a.appTheme.boldAll = !a.appTheme.boldAll
		if a.appTheme.boldAll {
			boldItem.Label = "Bold: On"
		} else {
			boldItem.Label = "Bold: Off"
		}
		a.fyneApp.Settings().SetTheme(a.appTheme)
	})

	var modeItem *fyne.MenuItem
	modeItem = fyne.NewMenuItem("Switch to Light Mode", func() {
		a.appTheme.dark = !a.appTheme.dark
		if a.appTheme.dark {
			modeItem.Label = "Switch to Light Mode"
		} else {
			modeItem.Label = "Switch to Dark Mode"
		}
		a.fyneApp.Settings().SetTheme(a.appTheme)
	})

	return fyne.NewMainMenu(
		fyne.NewMenu("View",
			larger, smaller, boldItem,
			fyne.NewMenuItemSeparator(),
			fontItem,
			fyne.NewMenuItemSeparator(),
			modeItem,
		),
	)
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
