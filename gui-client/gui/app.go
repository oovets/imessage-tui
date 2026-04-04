package gui

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oovets/bluebubbles-gui/api"
	"github.com/oovets/bluebubbles-gui/config"
	"github.com/oovets/bluebubbles-gui/models"
	"github.com/oovets/bluebubbles-gui/ws"
	"github.com/google/uuid"
)

// App is the top-level GUI application.
type App struct {
	fyneApp  fyne.App
	win      fyne.Window
	appTheme *compactTheme

	apiClient *api.Client
	wsClient  *ws.Client

	chatListComp   *ChatList
	paneManager    *PaneManager
	chatListPane   *fixedWidthWrap
	unreadBadge    *widget.Label
	unreadBadgeBox *fyne.Container
	contentHolder  *fyne.Container
	showChatList   bool
	unreadAnim     *fyne.Animation

	linkPreviewsEnabled bool
	maxLinkPreviews     int
	windowWidth         float32
	windowHeight        float32
	launchChatGUID      string
	detachedPaneMode    bool
	serverURL           string

	mu       sync.Mutex
	msgCache map[string][]models.Message

	wsMarkMu          sync.Mutex
	wsPendingChatGUID map[string]struct{}
	wsMarkTimer       *time.Timer

	wsRefreshMu          sync.Mutex
	wsPendingRefreshGUID map[string]struct{}
	wsRefreshTimer       *time.Timer
}

const fixedChatListWidth = float32(105)
const appPreferencesID = "com.bluebubbles-tui.gui"
const initialMessageFetchLimit = 50

const (
	prefShowChatList       = "ui.show_chat_list"
	prefDarkMode           = "ui.dark_mode"
	prefFontSize           = "ui.font_size"
	prefBoldAll            = "ui.bold_all"
	prefFontFamily         = "ui.font_family"
	prefEnableLinkPreviews = "ui.enable_link_previews"
	prefMaxLinkPreviews    = "ui.max_link_previews"
	prefCompactMode        = "ui.compact_mode"
	prefWindowWidth        = "ui.window_width"
	prefWindowHeight       = "ui.window_height"
	prefPaneLayoutState    = "ui.pane_layout_state"
)

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
func NewApp(apiClient *api.Client, wsClient *ws.Client, cfg *config.Config, launchChatGUID string, detachedPaneMode bool) *App {
	enablePreviews := true
	maxPreviews := 2
	serverURL := ""
	if cfg != nil {
		enablePreviews = cfg.EnableLinkPreviews
		maxPreviews = cfg.MaxPreviewsPerMessage
		serverURL = strings.TrimSpace(cfg.ServerURL)
	}
	if maxPreviews < 0 {
		maxPreviews = 0
	}

	return &App{
		apiClient:            apiClient,
		wsClient:             wsClient,
		msgCache:             make(map[string][]models.Message),
		wsPendingChatGUID:    make(map[string]struct{}),
		wsPendingRefreshGUID: make(map[string]struct{}),
		linkPreviewsEnabled:  enablePreviews,
		maxLinkPreviews:      maxPreviews,
		launchChatGUID:       strings.TrimSpace(launchChatGUID),
		detachedPaneMode:     detachedPaneMode,
		serverURL:            serverURL,
	}
}

// Run builds the window and blocks until the window is closed.
func (a *App) Run() {
	loadAliasStore()

	a.fyneApp = app.NewWithID(appPreferencesID)
	a.appTheme = newCompactTheme()
	a.loadUIState()
	a.fyneApp.Settings().SetTheme(a.appTheme)

	a.win = a.fyneApp.NewWindow("BlueBubbles")
	a.win.Resize(fyne.NewSize(a.windowWidth, a.windowHeight))

	setLinkPreviewEnabled(a.linkPreviewsEnabled)
	setMaxLinkPreviewsPerMessage(a.maxLinkPreviews)
	setLinkPreviewFetcherFromAPI(a.apiClient)
	setAttachmentFetcherFromAPI(a.apiClient)

	a.chatListComp = NewChatList(func(chat *models.Chat) {
		a.selectChat(chat)
	})
	a.chatListComp.onRename = func(guid string) {
		a.refreshPaneNameForChat(guid)
	}
	a.chatListPane = newFixedWidthWrap(a.chatListComp.Widget(), fixedChatListWidth)
	a.unreadBadge = widget.NewLabel("")
	a.unreadBadge.Importance = widget.HighImportance
	a.unreadBadge.Hide()
	a.unreadBadgeBox = container.NewPadded(a.unreadBadge)

	a.paneManager = NewPaneManager(
		func(pane *ChatPane, text string, replyTo *models.Message) { a.sendMessageFromPane(pane, text, replyTo) },
		func(pane *ChatPane, msg models.Message, reaction string) { a.sendReactionFromPane(pane, msg, reaction) },
		func(pane *ChatPane) {
			if pane == nil || a.win == nil {
				return
			}
			a.syncChatListSelectionWithPane(pane)
			if a.paneManager == nil || a.paneManager.FocusedPane() != pane {
				return
			}
			if !a.paneManager.AppFocused() || pane.IsInputFocused() {
				return
			}
			a.focusPaneInputWithRetry(pane)
		},
		a.handleInputShortcut,
	)
	if a.detachedPaneMode {
		a.showChatList = false
	} else {
		a.restorePaneLayoutState()
	}

	a.contentHolder = container.NewMax(a.mainContent())
	a.win.SetContent(a.contentHolder)
	a.refreshUnreadBadge()
	a.fyneApp.Lifecycle().SetOnExitedForeground(func() {
		fyne.Do(func() {
			a.paneManager.SetAppFocused(false)
			a.scrollAllPanes()
		})
	})
	a.fyneApp.Lifecycle().SetOnEnteredForeground(func() {
		fyne.Do(func() {
			a.paneManager.SetAppFocused(true)
			a.focusFocusedPaneInput()
			a.scrollAllPanes()
		})
	})
	// Keyboard shortcuts ─────────────────────────────────────────────────
	// Ctrl+N  open new GUI window
	// Ctrl+O  move focused pane chat to a new window
	// Ctrl+H  split focused pane side by side (horizontal)
	// Ctrl+J  split focused pane top/bottom   (vertical)
	// Ctrl+W  close focused pane
	c := a.win.Canvas()
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("N"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.openNewWindow()
	})
	c.AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyName("O"),
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) {
		a.moveFocusedPaneToNewWindow()
	})
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
	a.win.SetOnClosed(func() {
		a.saveWindowSizePreference()
		a.savePaneLayoutState()
	})

	a.win.ShowAndRun()
}

func (a *App) sendReactionFromPane(pane *ChatPane, target models.Message, reaction string) {
	reaction = strings.TrimSpace(reaction)
	if pane == nil || target.GUID == "" || !isReactionType(reaction) {
		return
	}
	chatGUID := strings.TrimSpace(pane.ChatGUID)
	if chatGUID == "" {
		chatGUID = strings.TrimSpace(target.ChatGUID)
	}
	if chatGUID == "" {
		return
	}

	pendingGUID := "pending-reaction-" + uuid.New().String()
	optimistic := models.Message{
		GUID:                  pendingGUID,
		IsFromMe:              true,
		DateCreated:           time.Now().UnixMilli(),
		ChatGUID:              chatGUID,
		AssociatedMessageGUID: "p:0/" + target.GUID,
		AssociatedMessageType: reaction,
	}

	a.mu.Lock()
	a.msgCache[chatGUID] = append(a.msgCache[chatGUID], optimistic)
	snapshot := a.sortedCacheSnapshot(chatGUID)
	a.mu.Unlock()

	fyne.Do(func() {
		if pane.ChatGUID == chatGUID {
			pane.msgView.SetMessages(snapshot)
			pane.inputArea.SetTransientStatus("Reaction queued")
		}
	})

	go func() {
		if err := a.apiClient.SendReaction(chatGUID, target.GUID, reaction, 0); err != nil {
			log.Printf("[GUI] SendReaction error: %v", err)
			a.mu.Lock()
			a.removePending(chatGUID, pendingGUID)
			snapshot := a.sortedCacheSnapshot(chatGUID)
			a.mu.Unlock()
			fyne.Do(func() {
				if pane.ChatGUID == chatGUID {
					pane.msgView.SetMessages(snapshot)
					pane.inputArea.SetTransientStatus("Reaction failed")
				}
			})
		}
	}()
}

func (a *App) syncChatListSelectionWithPane(pane *ChatPane) {
	if a == nil || a.chatListComp == nil || pane == nil {
		return
	}
	a.chatListComp.SetSelected(strings.TrimSpace(pane.ChatGUID))
}

func (a *App) splitFocusedHorizontal() {
	a.paneManager.SplitFocused(splitHorizontal)
	a.savePaneLayoutState()
	a.focusFocusedPaneInput()
	a.scrollAllPanes()
}

func (a *App) splitFocusedVertical() {
	a.paneManager.SplitFocused(splitVertical)
	a.savePaneLayoutState()
	a.focusFocusedPaneInput()
	a.scrollAllPanes()
}

func (a *App) focusFocusedPaneInput() {
	p := a.paneManager.FocusedPane()
	if p == nil || a.win == nil {
		return
	}
	a.focusPaneInputWithRetry(p)
}

func (a *App) focusPaneInputWithRetry(p *ChatPane) {
	if p == nil || a.win == nil || a.paneManager == nil {
		return
	}
	// Use a short timer so the canvas has a chance to register the new layout
	// before we request focus. An immediate call would fail if the widget tree
	// was just rebuilt (e.g. after a split), producing Fyne "not in canvas" errors.
	time.AfterFunc(20*time.Millisecond, func() {
		fyne.Do(func() {
			if a.win == nil || a.paneManager == nil {
				return
			}
			if !a.paneManager.AppFocused() || a.paneManager.FocusedPane() != p || p.IsInputFocused() {
				return
			}
			p.FocusInput(a.win.Canvas())
		})
	})
}

func (a *App) closeFocusedPane() {
	a.paneManager.CloseFocused()
	a.savePaneLayoutState()
	a.scrollAllPanes()
}

func (a *App) toggleChatListVisibility() {
	if a.detachedPaneMode {
		return
	}
	a.showChatList = !a.showChatList
	a.fyneApp.Preferences().SetBool(prefShowChatList, a.showChatList)
	a.contentHolder.Objects = []fyne.CanvasObject{a.mainContent()}
	a.contentHolder.Refresh()
	a.refreshUnreadBadge()
}

func (a *App) mainContent() fyne.CanvasObject {
	base := a.paneManager.Widget()
	if a.showChatList {
		base = container.NewBorder(nil, nil, a.chatListPane, nil, base)
	}
	topRight := container.NewPadded(container.NewHBox(layout.NewSpacer(), a.newOverflowMenuButton()))
	return container.NewStack(base, container.NewBorder(topRight, nil, nil, nil, nil))
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
	a.refreshUnreadBadge()
}

func (a *App) refreshAllMessageViews() {
	a.savePaneLayoutState()
	for _, p := range a.paneManager.AllPanes() {
		msgs := append([]models.Message(nil), p.msgView.messages...)
		p.msgView.SetMessages(msgs)
		p.RefreshLayout()
	}
}

func (a *App) refreshUnreadBadge() {
	if a.unreadBadge == nil || a.chatListComp == nil {
		return
	}
	count := a.chatListComp.UnreadChatsCount()
	if count <= 0 || a.showChatList {
		a.animateUnreadBadge(false, count)
		return
	}
	a.animateUnreadBadge(true, count)
}

func (a *App) animateUnreadBadge(show bool, count int) {
	if a.unreadBadge == nil {
		return
	}
	if a.unreadAnim != nil {
		a.unreadAnim.Stop()
	}
	if show {
		a.unreadBadge.SetText("• " + strconv.Itoa(count))
		a.unreadBadge.Show()
		if a.unreadBadgeBox != nil {
			a.unreadBadgeBox.Refresh()
		}
		a.unreadAnim = fyne.NewAnimation(140*time.Millisecond, func(f float32) {
			alpha := uint8(80 + f*175)
			a.unreadBadge.Importance = widget.MediumImportance
			a.unreadBadge.TextStyle = fyne.TextStyle{Bold: alpha > 180}
			a.unreadBadge.Refresh()
		})
		a.unreadAnim.Curve = fyne.AnimationEaseOut
		a.unreadAnim.Start()
		return
	}
	startVisible := a.unreadBadge.Visible()
	if !startVisible {
		return
	}
	a.unreadAnim = fyne.NewAnimation(110*time.Millisecond, func(f float32) {
		a.unreadBadge.Importance = widget.LowImportance
		a.unreadBadge.Refresh()
	})
	a.unreadAnim.Curve = fyne.AnimationEaseIn
	a.unreadAnim.Start()
	time.AfterFunc(110*time.Millisecond, func() {
		fyne.Do(func() {
			if a.chatListComp == nil || a.unreadBadge == nil {
				return
			}
			if a.showChatList || a.chatListComp.UnreadChatsCount() == 0 {
				a.unreadBadge.Hide()
				if a.unreadBadgeBox != nil {
					a.unreadBadgeBox.Refresh()
				}
			}
		})
	})
}

func (a *App) saveWindowSizePreference() {
	if a.fyneApp == nil || a.win == nil {
		return
	}
	size := a.win.Canvas().Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	prefs := a.fyneApp.Preferences()
	prefs.SetFloat(prefWindowWidth, float64(size.Width))
	prefs.SetFloat(prefWindowHeight, float64(size.Height))
}

func (a *App) savePaneLayoutState() {
	if a.detachedPaneMode {
		return
	}
	if a.fyneApp == nil || a.paneManager == nil {
		return
	}
	raw, err := a.paneManager.SerializeState()
	if err != nil {
		log.Printf("[GUI] serialize pane state failed: %v", err)
		return
	}
	a.fyneApp.Preferences().SetString(prefPaneLayoutState, raw)
}

func (a *App) restorePaneLayoutState() {
	if a.fyneApp == nil || a.paneManager == nil {
		return
	}
	raw := a.fyneApp.Preferences().StringWithFallback(prefPaneLayoutState, "")
	if strings.TrimSpace(raw) == "" {
		return
	}
	if err := a.paneManager.RestoreState(raw); err != nil {
		log.Printf("[GUI] restore pane state failed: %v", err)
	}
}

func (a *App) setLinkPreviewsEnabled(enabled bool) {
	a.linkPreviewsEnabled = enabled
	setLinkPreviewEnabled(enabled)
	a.fyneApp.Preferences().SetBool(prefEnableLinkPreviews, enabled)
	a.refreshAllMessageViews()
}

func (a *App) setMaxLinkPreviews(max int) {
	if max < 0 {
		max = 0
	}
	a.maxLinkPreviews = max
	setMaxLinkPreviewsPerMessage(max)
	a.fyneApp.Preferences().SetInt(prefMaxLinkPreviews, max)
	a.refreshAllMessageViews()
}

func (a *App) setDarkMode(enabled bool) {
	if a.appTheme == nil {
		return
	}
	a.appTheme.dark = enabled
	a.fyneApp.Preferences().SetBool(prefDarkMode, enabled)
	a.fyneApp.Settings().SetTheme(a.appTheme)
	// Refresh custom-painted pane elements (e.g. floating input background)
	// that do not automatically pick up theme changes.
	a.refreshAllMessageViews()
}

func (a *App) setCompactMode(enabled bool) {
	if a.appTheme == nil {
		return
	}
	a.appTheme.compactMode = enabled
	a.fyneApp.Preferences().SetBool(prefCompactMode, enabled)
	a.fyneApp.Settings().SetTheme(a.appTheme)
	a.refreshAllMessageViews()
}

// refreshPaneNameForChat updates the header of any open pane showing chatGUID
// after an alias change.
func (a *App) refreshPaneNameForChat(guid string) {
	for _, c := range a.chatListComp.chats {
		if c.GUID != guid {
			continue
		}
		name := chatDisplayName(c)
		for _, p := range a.paneManager.AllPanes() {
			if p.ChatGUID == guid {
				p.msgView.SetChatName(name)
			}
		}
		return
	}
}

func (a *App) setFont(family string) {
	if a.appTheme == nil {
		return
	}
	a.appTheme.curFamily = family
	a.fyneApp.Preferences().SetString(prefFontFamily, family)
	a.fyneApp.Settings().SetTheme(a.appTheme)
}

func (a *App) loadUIState() {
	if a.fyneApp == nil || a.appTheme == nil {
		return
	}
	prefs := a.fyneApp.Preferences()

	a.showChatList = prefs.BoolWithFallback(prefShowChatList, true)
	a.linkPreviewsEnabled = prefs.BoolWithFallback(prefEnableLinkPreviews, a.linkPreviewsEnabled)
	a.maxLinkPreviews = prefs.IntWithFallback(prefMaxLinkPreviews, a.maxLinkPreviews)
	if a.maxLinkPreviews < 0 {
		a.maxLinkPreviews = 0
	}

	a.appTheme.dark = prefs.BoolWithFallback(prefDarkMode, a.appTheme.dark)
	fontSize := prefs.IntWithFallback(prefFontSize, int(a.appTheme.fontSize))
	if fontSize < minUIFontSize {
		fontSize = minUIFontSize
	}
	if fontSize > maxUIFontSize {
		fontSize = maxUIFontSize
	}
	a.appTheme.fontSize = float32(fontSize)
	a.appTheme.boldAll = prefs.BoolWithFallback(prefBoldAll, a.appTheme.boldAll)
	a.appTheme.compactMode = prefs.BoolWithFallback(prefCompactMode, a.appTheme.compactMode)
	a.windowWidth = float32(prefs.FloatWithFallback(prefWindowWidth, 960))
	a.windowHeight = float32(prefs.FloatWithFallback(prefWindowHeight, 640))
	if a.windowWidth < 640 {
		a.windowWidth = 640
	}
	if a.windowHeight < 420 {
		a.windowHeight = 420
	}

	if family := prefs.StringWithFallback(prefFontFamily, a.appTheme.curFamily); family != "" {
		if _, ok := a.appTheme.fonts[family]; ok {
			a.appTheme.curFamily = family
		}
	}
}

func (a *App) buildOverflowMenu() *fyne.Menu {
	previewLabel := "Disable Previews"
	if !a.linkPreviewsEnabled {
		previewLabel = "Enable Previews"
	}

	colorModeLabel := "Switch to Light Mode"
	if !a.appTheme.dark {
		colorModeLabel = "Switch to Dark Mode"
	}
	compactLabel := "Enable Compact Mode"
	if a.appTheme.compactMode {
		compactLabel = "Disable Compact Mode"
	}
	// Build font submenu from installed families.
	fontItems := make([]*fyne.MenuItem, 0)
	for _, name := range a.appTheme.availableFamilies() {
		n := name // capture loop variable
		label := n
		if n == a.appTheme.curFamily {
			label = "✓ " + n
		}
		fontItems = append(fontItems, fyne.NewMenuItem(label, func() {
			a.setFont(n)
		}))
	}
	fontItem := fyne.NewMenuItem("Font", nil)
	fontItem.ChildMenu = fyne.NewMenu("", fontItems...)

	return fyne.NewMenu("",
		fyne.NewMenuItem("Settings...", func() {
			a.openSettingsDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("New Window", func() {
			a.openNewWindow()
		}),
		fyne.NewMenuItem("Move Focused Pane to New Window", func() {
			a.moveFocusedPaneToNewWindow()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("A+ Larger", func() {
			if a.appTheme.fontSize < maxUIFontSize {
				a.appTheme.fontSize++
				a.fyneApp.Preferences().SetInt(prefFontSize, int(a.appTheme.fontSize))
				a.fyneApp.Settings().SetTheme(a.appTheme)
				a.refreshChatListWidth()
				a.refreshAllMessageViews()
			}
		}),
		fyne.NewMenuItem("A- Smaller", func() {
			if a.appTheme.fontSize > minUIFontSize {
				a.appTheme.fontSize--
				a.fyneApp.Preferences().SetInt(prefFontSize, int(a.appTheme.fontSize))
				a.fyneApp.Settings().SetTheme(a.appTheme)
				a.refreshChatListWidth()
				a.refreshAllMessageViews()
			}
		}),
		fyne.NewMenuItem("Toggle Bold", func() {
			a.appTheme.boldAll = !a.appTheme.boldAll
			a.fyneApp.Preferences().SetBool(prefBoldAll, a.appTheme.boldAll)
			a.fyneApp.Settings().SetTheme(a.appTheme)
		}),
		fontItem,
		fyne.NewMenuItem(compactLabel, func() {
			a.setCompactMode(!a.appTheme.compactMode)
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
}

func (a *App) openSettingsDialog() {
	if a.win == nil || a.fyneApp == nil || a.appTheme == nil {
		return
	}

	appearance := a.buildAppearanceSettingsPane()
	behavior := a.buildBehaviorSettingsPane()
	previews := a.buildPreviewSettingsPane()
	connection := a.buildConnectionSettingsPane()

	tabs := container.NewAppTabs(
		container.NewTabItem("Appearance", appearance),
		container.NewTabItem("Behavior", behavior),
		container.NewTabItem("Previews", previews),
		container.NewTabItem("Connection", connection),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	d := dialog.NewCustom("Settings", "Close", container.NewPadded(tabs), a.win)
	d.Resize(fyne.NewSize(620, 420))
	d.Show()
}

func (a *App) buildAppearanceSettingsPane() fyne.CanvasObject {
	mode := widget.NewRadioGroup([]string{"Dark", "Light"}, func(v string) {
		a.setDarkMode(v == "Dark")
	})
	if a.appTheme.dark {
		mode.SetSelected("Dark")
	} else {
		mode.SetSelected("Light")
	}

	compact := widget.NewCheck("Compact layout", func(v bool) {
		a.setCompactMode(v)
	})
	compact.SetChecked(a.appTheme.compactMode)

	bold := widget.NewCheck("Bold text", func(v bool) {
		a.appTheme.boldAll = v
		a.fyneApp.Preferences().SetBool(prefBoldAll, a.appTheme.boldAll)
		a.fyneApp.Settings().SetTheme(a.appTheme)
		a.refreshAllMessageViews()
	})
	bold.SetChecked(a.appTheme.boldAll)

	fontSizeLabel := widget.NewLabel("Font size")
	fontSizeValue := widget.NewLabel(strconv.Itoa(int(a.appTheme.fontSize)))
	fontSize := widget.NewSlider(float64(minUIFontSize), float64(maxUIFontSize))
	fontSize.Step = 1
	fontSize.SetValue(float64(a.appTheme.fontSize))
	fontSize.OnChanged = func(v float64) {
		size := int(v)
		fontSizeValue.SetText(strconv.Itoa(size))
		a.appTheme.fontSize = float32(size)
		a.fyneApp.Preferences().SetInt(prefFontSize, size)
		a.fyneApp.Settings().SetTheme(a.appTheme)
		a.refreshChatListWidth()
		a.refreshAllMessageViews()
	}

	families := a.appTheme.availableFamilies()
	fontSelect := widget.NewSelect(families, func(v string) {
		if v != "" {
			a.setFont(v)
		}
	})
	fontSelect.SetSelected(a.appTheme.curFamily)

	form := widget.NewForm(
		widget.NewFormItem("Color mode", mode),
		widget.NewFormItem("Font family", fontSelect),
		widget.NewFormItem("", compact),
		widget.NewFormItem("", bold),
		widget.NewFormItem("", container.NewBorder(nil, nil, fontSizeLabel, fontSizeValue, fontSize)),
	)

	return container.NewPadded(form)
}

func (a *App) buildBehaviorSettingsPane() fyne.CanvasObject {
	chatListToggle := widget.NewCheck("Show chat list", func(v bool) {
		if a.detachedPaneMode {
			return
		}
		if v != a.showChatList {
			a.toggleChatListVisibility()
		}
	})
	chatListToggle.SetChecked(a.showChatList)
	if a.detachedPaneMode {
		chatListToggle.Disable()
	}

	newWindowBtn := widget.NewButton("Open New Window", func() {
		a.openNewWindow()
	})
	detachBtn := widget.NewButton("Move Focused Pane To New Window", func() {
		a.moveFocusedPaneToNewWindow()
	})

	return container.NewPadded(container.NewVBox(
		chatListToggle,
		widget.NewSeparator(),
		newWindowBtn,
		detachBtn,
	))
}

func (a *App) buildPreviewSettingsPane() fyne.CanvasObject {
	enabled := widget.NewCheck("Enable link previews", func(v bool) {
		a.setLinkPreviewsEnabled(v)
	})
	enabled.SetChecked(a.linkPreviewsEnabled)

	maxLabel := widget.NewLabel("Max previews per message")
	maxSelect := widget.NewRadioGroup([]string{"0", "1", "2"}, func(v string) {
		n, err := strconv.Atoi(v)
		if err != nil {
			return
		}
		a.setMaxLinkPreviews(n)
	})
	maxSelect.Horizontal = true
	maxSelect.SetSelected(strconv.Itoa(a.maxLinkPreviews))

	return container.NewPadded(container.NewVBox(
		enabled,
		maxLabel,
		maxSelect,
	))
}

func (a *App) buildConnectionSettingsPane() fyne.CanvasObject {
	server := strings.TrimSpace(a.serverURL)
	if server == "" {
		server = "Unknown"
	}

	serverLabel := widget.NewLabel("Server")
	serverValue := widget.NewLabel(server)
	serverValue.Wrapping = fyne.TextWrapWord

	clearBtn := widget.NewButton("Clear saved password", func() {
		if err := config.ClearStoredPassword(); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		dialog.ShowInformation("Password cleared", "Saved password removed. Restart GUI to run first-time setup again.", a.win)
	})

	return container.NewPadded(container.NewVBox(
		container.NewBorder(nil, nil, nil, nil, container.NewVBox(serverLabel, serverValue)),
		widget.NewSeparator(),
		clearBtn,
	))
}

func (a *App) newOverflowMenuButton() fyne.CanvasObject {
	btn := widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), nil)
	btn.Importance = widget.LowImportance
	btn.OnTapped = func() {
		c := fyne.CurrentApp().Driver().CanvasForObject(btn)
		if c == nil {
			return
		}
		widget.ShowPopUpMenuAtRelativePosition(
			a.buildOverflowMenu(),
			c,
			fyne.NewPos(0, btn.Size().Height),
			btn,
		)
	}
	return btn
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
	case fyne.KeyName("N"):
		a.openNewWindow()
		return true
	case fyne.KeyName("O"):
		a.moveFocusedPaneToNewWindow()
		return true
	case fyne.KeyName("H"):
		a.splitFocusedHorizontal()
		return true
	case fyne.KeyName("J"):
		a.splitFocusedVertical()
		return true
	case fyne.KeyName("S"):
		a.toggleChatListVisibility()
		return true
	case fyne.KeyName("W"):
		a.closeFocusedPane()
		return true
	default:
		return false
	}
}

func (a *App) openNewWindow() {
	_ = a.launchWindowForChat("", false)
}

func (a *App) openFocusedPaneInNewWindow() {
	if a.paneManager == nil {
		a.openNewWindow()
		return
	}
	p := a.paneManager.FocusedPane()
	if p == nil || strings.TrimSpace(p.ChatGUID) == "" {
		a.openNewWindow()
		return
	}
	_ = a.launchWindowForChat(p.ChatGUID, false)
}

func (a *App) moveFocusedPaneToNewWindow() {
	if a.paneManager == nil {
		a.openNewWindow()
		return
	}
	p := a.paneManager.FocusedPane()
	if p == nil || strings.TrimSpace(p.ChatGUID) == "" {
		a.openNewWindow()
		return
	}
	if !a.launchWindowForChat(p.ChatGUID, true) {
		return
	}
	if len(a.paneManager.AllPanes()) > 1 {
		a.closeFocusedPane()
		return
	}
	// Single-pane window: "move" means the current pane no longer shows the chat.
	p.ChatGUID = ""
	p.ClearReplyTarget()
	p.msgView.SetMessages(nil)
	if a.chatListComp != nil {
		a.chatListComp.SetSelected("")
	}
	a.savePaneLayoutState()
}

func (a *App) launchWindowForChat(chatGUID string, detachedPaneMode bool) bool {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		log.Printf("[GUI] Failed to resolve executable for new window: %v", err)
		return false
	}
	args := []string{}
	if cg := strings.TrimSpace(chatGUID); cg != "" {
		args = append(args, "--chat-guid", cg)
	}
	if detachedPaneMode {
		args = append(args, "--detached-pane")
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		log.Printf("[GUI] Failed to launch new window: %v", err)
		return false
	}
	return true
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
	a.savePaneLayoutState()

	a.chatListComp.ClearNewMessage(chatGUID)
	a.refreshUnreadBadge()
	a.chatListComp.SetSelected(chatGUID)
	pane.msgView.SetChatName(chatDisplayName(*chat))
	pane.msgView.SetLoading()
	pane.FocusInput(a.win.Canvas())

	go a.loadMessagesForPane(pane, chatGUID)
}

// loadMessagesForPane fetches messages and updates the given pane.
func (a *App) loadMessagesForPane(pane *ChatPane, chatGUID string) {
	defer perfStart("loadMessagesForPane")()
	msgs, err := a.apiClient.GetMessages(chatGUID, initialMessageFetchLimit)
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
	defer perfStart("sendMessageFromPane")()
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
		a.chatListComp.ApplyMessageActivity(chatGUID, optimistic, false)
		if pane.ChatGUID == chatGUID {
			pane.msgView.SetMessages(snapshot)
			pane.inputArea.SetTransientStatus("Message queued")
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
					pane.inputArea.SetTransientStatus("Send failed")
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
	defer perfStart("loadChats")()
	chats, err := a.apiClient.GetChats(50)
	if err != nil {
		log.Printf("[GUI] GetChats error: %v", err)
		return
	}

	fyne.Do(func() {
		a.chatListComp.SetChats(chats)
		a.refreshUnreadBadge()
		if a.launchChatGUID != "" {
			for i := range chats {
				if chats[i].GUID != a.launchChatGUID {
					continue
				}
				a.chatListComp.SetSelected(chats[i].GUID)
				a.selectChat(&chats[i])
				return
			}
			log.Printf("[GUI] launch chat GUID not found in chat list: %s", a.launchChatGUID)
		}
		hasAssigned := false
		for _, p := range a.paneManager.AllPanes() {
			if p.ChatGUID == "" {
				continue
			}
			for i := range chats {
				if chats[i].GUID == p.ChatGUID {
					hasAssigned = true
					p.msgView.SetMessages(nil)
					go a.loadMessagesForPane(p, p.ChatGUID)
					break
				}
			}
		}
		if !hasAssigned && len(chats) > 0 {
			first := chats[0]
			a.chatListComp.SetSelected(first.GUID)
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
	defer perfStart("handleWSEvent")()
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
				if !removed && strings.HasPrefix(m.GUID, "pending-") {
					sameText := m.Text != "" && m.Text == msg.Text
					sameReaction := isReactionType(m.AssociatedMessageType) &&
						m.AssociatedMessageType == msg.AssociatedMessageType &&
						normalizeAssociatedMessageGUID(m.AssociatedMessageGUID) == normalizeAssociatedMessageGUID(msg.AssociatedMessageGUID)
					if sameText || sameReaction {
						removed = true
						continue
					}
				}
				out = append(out, m)
			}
			cached = out
		}
		a.msgCache[msg.ChatGUID] = append(cached, msg)
		a.mu.Unlock()

		fyne.Do(func() {
			markUnread := !msg.IsFromMe && len(a.paneManager.PanesShowingChat(msg.ChatGUID)) == 0
			a.chatListComp.ApplyMessageActivity(msg.ChatGUID, msg, markUnread)
			panesShowing := a.paneManager.PanesShowingChat(msg.ChatGUID)
			if len(panesShowing) > 0 {
				if msg.IsFromMe {
					a.queuePaneRefresh(msg.ChatGUID)
				} else {
					// Incoming messages can be appended without rebuilding full history.
					for _, p := range panesShowing {
						p.msgView.AppendMessage(msg)
					}
				}
			} else if markUnread {
				a.refreshUnreadBadge()
			}
		})
	case "updated-message", "message-updated":
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
		if msg.ChatGUID == "" || msg.GUID == "" {
			return
		}

		a.mu.Lock()
		cached := a.msgCache[msg.ChatGUID]
		replaced := false
		for i := range cached {
			if cached[i].GUID == msg.GUID {
				cached[i] = msg
				replaced = true
				break
			}
		}
		if !replaced {
			cached = append(cached, msg)
		}
		a.msgCache[msg.ChatGUID] = cached
		a.mu.Unlock()

		a.queuePaneRefresh(msg.ChatGUID)
	}
}

func (a *App) queuePaneRefresh(chatGUID string) {
	chatGUID = strings.TrimSpace(chatGUID)
	if chatGUID == "" {
		return
	}

	a.wsRefreshMu.Lock()
	a.wsPendingRefreshGUID[chatGUID] = struct{}{}
	if a.wsRefreshTimer != nil {
		a.wsRefreshMu.Unlock()
		return
	}
	a.wsRefreshTimer = time.AfterFunc(70*time.Millisecond, a.flushPaneRefreshes)
	a.wsRefreshMu.Unlock()
}

func (a *App) flushPaneRefreshes() {
	a.wsRefreshMu.Lock()
	pending := make([]string, 0, len(a.wsPendingRefreshGUID))
	for guid := range a.wsPendingRefreshGUID {
		pending = append(pending, guid)
	}
	a.wsPendingRefreshGUID = make(map[string]struct{})
	a.wsRefreshTimer = nil
	a.wsRefreshMu.Unlock()

	if len(pending) == 0 {
		return
	}

	fyne.Do(func() {
		for _, guid := range pending {
			panesShowing := a.paneManager.PanesShowingChat(guid)
			if len(panesShowing) == 0 {
				continue
			}
			a.mu.Lock()
			snapshot := a.sortedCacheSnapshot(guid)
			a.mu.Unlock()
			for _, p := range panesShowing {
				p.msgView.SetMessages(snapshot)
			}
		}
	})
}

func (a *App) queueUnreadMark(chatGUID string) {
	chatGUID = strings.TrimSpace(chatGUID)
	if chatGUID == "" || a.chatListComp == nil {
		return
	}

	a.wsMarkMu.Lock()
	a.wsPendingChatGUID[chatGUID] = struct{}{}
	if a.wsMarkTimer != nil {
		a.wsMarkMu.Unlock()
		return
	}
	a.wsMarkTimer = time.AfterFunc(90*time.Millisecond, a.flushUnreadMarks)
	a.wsMarkMu.Unlock()
}

func (a *App) flushUnreadMarks() {
	a.wsMarkMu.Lock()
	pending := make([]string, 0, len(a.wsPendingChatGUID))
	for guid := range a.wsPendingChatGUID {
		pending = append(pending, guid)
	}
	a.wsPendingChatGUID = make(map[string]struct{})
	a.wsMarkTimer = nil
	a.wsMarkMu.Unlock()

	if len(pending) == 0 {
		return
	}

	fyne.Do(func() {
		if a.chatListComp == nil {
			return
		}
		for _, guid := range pending {
			a.chatListComp.MarkNewMessage(guid)
		}
		a.refreshUnreadBadge()
	})
}
