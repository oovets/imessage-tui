package gui

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/config"
	"github.com/bluebubbles-tui/models"
	"github.com/bluebubbles-tui/ws"
	"github.com/google/uuid"
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
	chatListPane  *fixedWidthWrap
	contentHolder *fyne.Container
	showChatList  bool

	linkPreviewsEnabled bool
	maxLinkPreviews     int

	mu       sync.Mutex
	msgCache map[string][]models.Message
}

const fixedChatListWidth = float32(105)

type fixedWidthWrap struct {
	widget.BaseWidget
	child fyne.CanvasObject
	width float32
}

func newFixedWidthWrap(child fyne.CanvasObject, width float32) *fixedWidthWrap {
	w := &fixedWidthWrap{child: child, width: width}
	w.ExtendBaseWidget(w)
	return w
}

func (w *fixedWidthWrap) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.child)
}

func (w *fixedWidthWrap) MinSize() fyne.Size {
	return fyne.NewSize(w.width, w.child.MinSize().Height)
}

func (w *fixedWidthWrap) SetWidth(width float32) {
	w.width = width
	w.Refresh()
}

// NewApp creates a new GUI application using the given API and WebSocket clients.
func NewApp(apiClient *api.Client, wsClient *ws.Client, cfg *config.Config) *App {
	enablePreviews := true
	maxPreviews := 2
	if cfg != nil {
		enablePreviews = cfg.EnableLinkPreviews
		maxPreviews = cfg.MaxPreviewsPerMessage
	}
	if maxPreviews < 0 {
		maxPreviews = 0
	}

	return &App{
		apiClient:           apiClient,
		wsClient:            wsClient,
		msgCache:            make(map[string][]models.Message),
		linkPreviewsEnabled: enablePreviews,
		maxLinkPreviews:     maxPreviews,
	}
}

// Run builds the window and blocks until the window is closed.
func (a *App) Run() {
	a.fyneApp = app.New()
	a.appTheme = newCompactTheme()
	a.fyneApp.Settings().SetTheme(a.appTheme)

	a.win = a.fyneApp.NewWindow("BlueBubbles")
	a.win.Resize(fyne.NewSize(960, 640))
	a.win.SetMainMenu(a.buildMainMenu())

	setLinkPreviewEnabled(a.linkPreviewsEnabled)
	setMaxLinkPreviewsPerMessage(a.maxLinkPreviews)
	setLinkPreviewFetcherFromAPI(a.apiClient)
	setAttachmentFetcherFromAPI(a.apiClient)

	a.chatListComp = NewChatList(func(chat *models.Chat) {
		a.selectChat(chat)
	})
	a.chatListPane = newFixedWidthWrap(a.chatListComp.Widget(), fixedChatListWidth)

	a.paneManager = NewPaneManager(
		func(pane *ChatPane, text string, replyTo *models.Message) { a.sendMessageFromPane(pane, text, replyTo) },
		func(pane *ChatPane) { /* focus tracked inside PaneManager */ },
		a.handleInputShortcut,
	)

	a.showChatList = true
	a.contentHolder = container.NewMax(a.mainContent())
	a.win.SetContent(a.contentHolder)
	a.focusFocusedPaneInput()

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
	go a.loadChats()
	go a.runWebSocket()

	a.win.ShowAndRun()
}

func (a *App) splitFocusedHorizontal() {
	a.paneManager.SplitFocused(splitHorizontal)
	a.focusFocusedPaneInput()
	a.scrollAllPanes()
}

func (a *App) splitFocusedVertical() {
	a.paneManager.SplitFocused(splitVertical)
	a.focusFocusedPaneInput()
	a.scrollAllPanes()
}

func (a *App) focusFocusedPaneInput() {
	p := a.paneManager.FocusedPane()
	if p == nil || a.win == nil {
		return
	}
	p.FocusInput(a.win.Canvas())
}

func (a *App) closeFocusedPane() {
	a.paneManager.CloseFocused()
	a.scrollAllPanes()
}

func (a *App) toggleChatListVisibility() {
	a.showChatList = !a.showChatList
	a.contentHolder.Objects = []fyne.CanvasObject{a.mainContent()}
	a.contentHolder.Refresh()
}

func (a *App) mainContent() fyne.CanvasObject {
	if !a.showChatList {
		return a.paneManager.Widget()
	}
	return container.NewBorder(nil, nil, a.chatListPane, nil, a.paneManager.Widget())
}

func (a *App) refreshChatListWidth() {
	if a.chatListPane == nil {
		return
	}
	a.chatListPane.SetWidth(fixedChatListWidth)
	if a.showChatList {
		a.contentHolder.Objects = []fyne.CanvasObject{a.mainContent()}
		a.contentHolder.Refresh()
	}
}

func (a *App) refreshAllMessageViews() {
	for _, p := range a.paneManager.AllPanes() {
		msgs := append([]models.Message(nil), p.msgView.messages...)
		p.msgView.SetMessages(msgs)
	}
}

func (a *App) setLinkPreviewsEnabled(enabled bool) {
	a.linkPreviewsEnabled = enabled
	setLinkPreviewEnabled(enabled)
	a.refreshAllMessageViews()
	a.win.SetMainMenu(a.buildMainMenu())
}

func (a *App) setMaxLinkPreviews(max int) {
	if max < 0 {
		max = 0
	}
	a.maxLinkPreviews = max
	setMaxLinkPreviewsPerMessage(max)
	a.refreshAllMessageViews()
	a.win.SetMainMenu(a.buildMainMenu())
}

func (a *App) setDarkMode(enabled bool) {
	if a.appTheme == nil {
		return
	}
	a.appTheme.dark = enabled
	a.fyneApp.Settings().SetTheme(a.appTheme)
	a.win.SetMainMenu(a.buildMainMenu())
}

func (a *App) buildMainMenu() *fyne.MainMenu {
	previewLabel := "Disable Previews"
	if !a.linkPreviewsEnabled {
		previewLabel = "Enable Previews"
	}

	colorModeLabel := "Switch to Light Mode"
	if !a.appTheme.dark {
		colorModeLabel = "Switch to Dark Mode"
	}

	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("A+ Larger", func() {
			if a.appTheme.fontSize < 20 {
				a.appTheme.fontSize++
				a.fyneApp.Settings().SetTheme(a.appTheme)
				a.refreshChatListWidth()
			}
		}),
		fyne.NewMenuItem("A- Smaller", func() {
			if a.appTheme.fontSize > 8 {
				a.appTheme.fontSize--
				a.fyneApp.Settings().SetTheme(a.appTheme)
				a.refreshChatListWidth()
			}
		}),
		fyne.NewMenuItem("Toggle Bold", func() {
			a.appTheme.boldAll = !a.appTheme.boldAll
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}),
		fyne.NewMenuItem(colorModeLabel, func() {
			a.setDarkMode(!a.appTheme.dark)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(previewLabel, func() {
			a.setLinkPreviewsEnabled(!a.linkPreviewsEnabled)
		}),
		fyne.NewMenuItem("Max Previews: 1", func() {
			a.setMaxLinkPreviews(1)
		}),
		fyne.NewMenuItem("Max Previews: 2", func() {
			a.setMaxLinkPreviews(2)
		}),
	)

	return fyne.NewMainMenu(viewMenu)
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
	pane.FocusInput(a.win.Canvas())

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
// The message is shown immediately (optimistic UI); the API call happens in the
// background. When the real message arrives via WebSocket the pending copy is
// swapped out transparently.
func (a *App) sendMessageFromPane(pane *ChatPane, text string, replyTo *models.Message) {
	chatGUID := pane.ChatGUID
	if chatGUID == "" {
		return
	}

	replyToGUID := ""
	if replyTo != nil {
		replyToGUID = replyTo.GUID
	}

	// 1. Inject an optimistic message so the UI updates before any network call.
	pendingGUID := "pending-" + uuid.New().String()
	optimistic := models.Message{
		GUID:        pendingGUID,
		Text:        text,
		IsFromMe:    true,
		DateCreated: time.Now().UnixMilli(),
		ChatGUID:    chatGUID,
	}

	a.mu.Lock()
	a.msgCache[chatGUID] = append(a.msgCache[chatGUID], optimistic)
	snapshot := a.sortedCacheSnapshot(chatGUID)
	a.mu.Unlock()

	fyne.Do(func() {
		if pane.ChatGUID == chatGUID {
			pane.msgView.SetMessages(snapshot)
		}
	})

	// 2. Fire-and-forget: send to server. WebSocket delivers the real message.
	go func() {
		if err := a.apiClient.SendMessage(chatGUID, text, replyToGUID); err != nil {
			log.Printf("[GUI] SendMessage error: %v", err)
			// Roll back the optimistic message on failure.
			a.mu.Lock()
			a.removePending(chatGUID, pendingGUID)
			snapshot := a.sortedCacheSnapshot(chatGUID)
			a.mu.Unlock()
			fyne.Do(func() {
				if pane.ChatGUID == chatGUID {
					pane.msgView.SetMessages(snapshot)
				}
			})
		}
	}()
}

// sortedCacheSnapshot returns a sorted copy of the message cache for chatGUID.
// Must be called with a.mu held.
func (a *App) sortedCacheSnapshot(chatGUID string) []models.Message {
	src := a.msgCache[chatGUID]
	out := make([]models.Message, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool { return out[i].DateCreated < out[j].DateCreated })
	return out
}

// removePending removes a single message by GUID from the cache.
// Must be called with a.mu held.
func (a *App) removePending(chatGUID, guid string) {
	cached := a.msgCache[chatGUID]
	out := cached[:0]
	for _, m := range cached {
		if m.GUID != guid {
			out = append(out, m)
		}
	}
	a.msgCache[chatGUID] = out
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
		// Check if already cached.
		for _, m := range cached {
			if m.GUID == msg.GUID {
				a.mu.Unlock()
				return
			}
		}
		// Remove any pending optimistic message with matching text (our own send).
		if msg.IsFromMe {
			out := cached[:0]
			removed := false
			for _, m := range cached {
				if !removed && strings.HasPrefix(m.GUID, "pending-") && m.Text == msg.Text {
					removed = true
					continue
				}
				out = append(out, m)
			}
			cached = out
		}
		a.msgCache[msg.ChatGUID] = append(cached, msg)
		snapshot := a.sortedCacheSnapshot(msg.ChatGUID)
		a.mu.Unlock()

		fyne.Do(func() {
			panesShowing := a.paneManager.PanesShowingChat(msg.ChatGUID)
			if len(panesShowing) > 0 {
				for _, p := range panesShowing {
					p.msgView.SetMessages(snapshot)
				}
			} else {
				a.chatListComp.MarkNewMessage(msg.ChatGUID)
			}
		})
	}
}
