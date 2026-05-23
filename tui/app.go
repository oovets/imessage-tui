package tui

import (
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/ws"
)

const defaultMessageFetchLimit = 50
const defaultChatFetchLimit = 50
const defaultPollInterval = 10 * time.Second
const messageCacheRefreshTTL = 20 * time.Second
const initialPrefetchChatCount = 5

// pendingOutgoingTimeout is how long we keep a local optimistic echo before
// assuming the server never delivered it back via WS. When it expires we drop
// the optimistic row and refetch the chat so the truth from the server wins.
const pendingOutgoingTimeout = 30 * time.Second

type focusRegion int

const (
	focusChatList focusRegion = iota
	focusWindow
)

type chatActionMode int

const (
	chatActionNone chatActionMode = iota
	chatActionDelete
	chatActionRename
)

// Message types for Bubble Tea
type (
	chatsLoadedMsg    []models.Chat
	messagesLoadedMsg struct {
		chatGUID string
		messages []models.Message
	}
	loadMessagesErrMsg struct {
		chatGUID string
		err      error
		report   bool
	}
	sendSuccessMsg struct {
		windowID WindowID
	}
	sendErrMsg struct {
		err         error
		chatGUID    string
		pendingGUID string
	}
	wsEventMsg          models.WSEvent
	wsConnectSuccessMsg struct{}
	wsConnectFailMsg    struct{ err error }
	wsReconnectedMsg    struct{}
	wsDisconnectedMsg   struct{}
	wsOverflowMsg       struct{}
	refreshTickMsg      struct{}
	markReadSuccessMsg  struct{ chatGUID string }
	markReadErrMsg      struct {
		chatGUID string
		err      error
	}
	pendingTimeoutMsg struct {
		chatGUID    string
		pendingGUID string
	}
	linkPreviewLoadedMsg struct {
		chatGUID    string
		messageGUID string
		url         string
		preview     models.LinkPreview
		err         error
	}
	deleteChatSuccessMsg struct{ chatGUID string }
	deleteChatErrMsg     struct {
		chatGUID string
		err      error
	}
	renameChatSuccessMsg struct {
		chatGUID    string
		displayName string
	}
	renameChatErrMsg struct {
		chatGUID    string
		displayName string
		err         error
	}
	errMsg          struct{ err error }
	noticeExpireMsg struct{}
	toastMsg        struct {
		text     string
		duration time.Duration
	}
)

type AppModel struct {
	// Sub-components
	chatList      ChatListModel
	windowManager *WindowManager

	// State
	loading         bool
	err             error
	wsConnected     bool
	lastRefreshTime time.Time

	// Clients
	apiClient *api.Client
	wsClient  *ws.Client

	// Terminal dimensions
	width  int
	height int

	// Focus tracking
	focused focusRegion

	// Debug
	lastKey string

	showTimestamps        bool
	showLineNumbers       bool
	showChatList          bool
	showSenderNames       bool
	showPaneDividers      bool
	showChatPreview       bool
	chatListWidth         int
	messageLimit          int
	chatLimit             int
	pollInterval          time.Duration
	enableLinkPreviews    bool
	maxPreviewsPerMessage int

	messageFetchInFlight map[string]bool
	messageFetchedAt     map[string]time.Time
	pendingOutgoing      map[string][]models.Message
	readMarkInFlight     map[string]bool
	linkPreviewInFlight  map[string]bool
	linkPreviewAttempted map[string]bool
	disableReadSync      bool
	savedLayoutState     *config.LayoutState
	didRestoreLayout     bool
	chatOverrides        config.ChatOverridesState

	showHelp         bool
	chatAction       chatActionMode
	actionChatGUID   string
	actionChatName   string
	renameText       string
	toastText        string
	toastUntil       time.Time
	errUntil         time.Time
	sidebarDragging  bool
	sidebarDragStart int
	dividerDragging  bool
	dividerNode      *LayoutNode

	// All disk writes go through this so the Update loop never blocks on
	// fsync/marshal. Blocking Update for even 50ms causes dropped
	// keystrokes during fast typing.
	persist *persister
}

func NewAppModel(client *api.Client, wsClient *ws.Client) AppModel {
	return NewAppModelWithConfig(client, wsClient, nil)
}

func NewAppModelWithConfig(client *api.Client, wsClient *ws.Client, cfg *config.Config) AppModel {
	ui := config.LoadUIState()
	chatOverrides := config.LoadChatOverrides()
	messageLimit := defaultMessageFetchLimit
	chatLimit := defaultChatFetchLimit
	pollInterval := defaultPollInterval
	enableLinkPreviews := true
	maxPreviewsPerMessage := 2
	if cfg != nil {
		if cfg.MessageLimit > 0 {
			messageLimit = cfg.MessageLimit
		}
		if cfg.ChatLimit > 0 {
			chatLimit = cfg.ChatLimit
		}
		if cfg.PollIntervalSec > 0 {
			pollInterval = time.Duration(cfg.PollIntervalSec) * time.Second
		} else {
			pollInterval = 0
		}
		enableLinkPreviews = cfg.EnableLinkPreviews
		if cfg.MaxPreviewsPerMessage > 0 {
			maxPreviewsPerMessage = cfg.MaxPreviewsPerMessage
		}
		if client != nil {
			client.SetPreviewProxyURL(cfg.PreviewProxyURL)
			client.SetOEmbedEndpoint(cfg.OEmbedEndpoint)
		}
	}

	wm := NewWindowManager()
	wm.SetShowTimestamps(ui.ShowTimestamps)
	wm.SetShowLineNumbers(ui.ShowLineNumbers)
	wm.SetShowSenderNames(ui.ShowSenderNames)
	wm.SetShowPaneDividers(ui.ShowPaneDividers)
	app := AppModel{
		chatList:              NewChatListModel(),
		windowManager:         wm,
		apiClient:             client,
		wsClient:              wsClient,
		focused:               focusChatList,
		width:                 80,
		height:                24,
		showTimestamps:        ui.ShowTimestamps,
		showLineNumbers:       ui.ShowLineNumbers,
		showChatList:          ui.ShowChatList,
		showSenderNames:       ui.ShowSenderNames,
		showPaneDividers:      ui.ShowPaneDividers,
		showChatPreview:       ui.ShowChatPreview,
		chatListWidth:         clampChatListWidth(ui.ChatListWidth),
		messageLimit:          messageLimit,
		chatLimit:             chatLimit,
		pollInterval:          pollInterval,
		enableLinkPreviews:    enableLinkPreviews,
		maxPreviewsPerMessage: maxPreviewsPerMessage,
		messageFetchInFlight:  make(map[string]bool),
		messageFetchedAt:      make(map[string]time.Time),
		pendingOutgoing:       make(map[string][]models.Message),
		readMarkInFlight:      make(map[string]bool),
		linkPreviewInFlight:   make(map[string]bool),
		linkPreviewAttempted:  make(map[string]bool),
		chatOverrides:         chatOverrides,
		persist:               newPersister(),
		loading:               true,
	}
	if layoutState, ok := config.LoadLayoutState(); ok {
		app.savedLayoutState = &layoutState
	}
	app.chatList.SetShowPreview(app.showChatPreview)
	app.restoreMessageCache()
	return app
}

func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		loadChatsCmd(m.apiClient, m.chatLimit),
		noticeExpireCmd(),
	}

	// Try to connect WebSocket for real-time updates
	if m.wsClient != nil {
		cmds = append(cmds, connectWSCmd(m.wsClient))
	}
	if m.pollInterval > 0 {
		cmds = append(cmds, refreshTickCmd(m.pollInterval))
	}

	return tea.Batch(cmds...)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case chatsLoadedMsg:
		m.loading = false
		m.chatList.SetLoading(false)
		chats := []models.Chat(msg)
		applyChatOverrides(chats, m.chatOverrides.Aliases)
		m.chatList.SetChats(chats)
		if m.restoreLayoutFromState(chats) {
			m.updateLayout()
			return m, nil
		}
		m.updateLayout()
		var cmds []tea.Cmd
		// Auto-select first chat in focused window if available
		if len(chats) > 0 {
			window := m.windowManager.FocusedWindow()
			if window != nil {
				chat := chats[0]
				m.focused = focusWindow
				window.Input.textarea.Focus()
				if cmd := m.selectChat(&chat, window); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if cmd := m.prefetchTopChatsCmd(chats, chat.GUID); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
			if cmd := m.prefetchTopChatsCmd(chats, ""); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if len(cmds) > 0 {
				return m, tea.Batch(cmds...)
			}
		}
		return m, nil

	case messagesLoadedMsg:
		// Merge API messages with any WS messages that arrived after the API snapshot.
		// This prevents a race where WS-appended messages disappear when the API
		// response (which may not yet include them) replaces the message list.
		merged := mergeLoadedMessagesWithCache(msg.messages, m.windowManager.GetCachedMessages(msg.chatGUID))
		m.windowManager.SetCachedMessages(msg.chatGUID, merged)
		m.messageFetchInFlight[msg.chatGUID] = false
		m.messageFetchedAt[msg.chatGUID] = time.Now()
		m.saveMessageCache()
		for _, window := range m.windowManager.WindowsShowingChat(msg.chatGUID) {
			window.Messages.SetMessages(merged)
			window.Messages.SetLoading(false)
		}
		return m, m.linkPreviewCmdsForMessages(msg.chatGUID, merged)

	case loadMessagesErrMsg:
		m.messageFetchInFlight[msg.chatGUID] = false
		for _, window := range m.windowManager.WindowsShowingChat(msg.chatGUID) {
			window.Messages.SetLoading(false)
		}
		if msg.report {
			m.setAppError(msg.err)
		}
		return m, nil

	case sendSuccessMsg:
		// Input was already cleared synchronously on Enter. Do not clear here:
		// the user may have started typing the next message while the send was
		// in flight.
		return m, nil

	case sendErrMsg:
		m.removePendingOutgoingByGUID(msg.chatGUID, msg.pendingGUID)
		m.setAppError(msg.err)
		return m, nil

	case noticeExpireMsg:
		now := time.Now()
		if !m.errUntil.IsZero() && now.After(m.errUntil) {
			m.err = nil
			m.errUntil = time.Time{}
		}
		if m.toastText != "" && !m.toastUntil.IsZero() && now.After(m.toastUntil) {
			m.toastText = ""
			m.toastUntil = time.Time{}
		}
		return m, noticeExpireCmd()

	case toastMsg:
		m.toastText = msg.text
		m.toastUntil = time.Now().Add(msg.duration)
		return m, nil

	case pendingTimeoutMsg:
		// Only act if the pending entry is still there (otherwise the WS
		// echo already resolved it). When it times out we refetch the chat
		// so the server's view replaces our optimistic guess.
		if !m.hasPendingOutgoing(msg.chatGUID, msg.pendingGUID) {
			return m, nil
		}
		m.removePendingOutgoingByGUID(msg.chatGUID, msg.pendingGUID)
		if m.messageFetchInFlight[msg.chatGUID] {
			return m, nil
		}
		m.messageFetchInFlight[msg.chatGUID] = true
		return m, prefetchMessagesCmd(m.apiClient, msg.chatGUID, m.messageLimit)

	case linkPreviewLoadedMsg:
		key := linkPreviewKey(msg.messageGUID, msg.url)
		if m.linkPreviewAttempted == nil {
			m.linkPreviewAttempted = make(map[string]bool)
		}
		delete(m.linkPreviewInFlight, key)
		m.linkPreviewAttempted[key] = true
		if msg.err != nil {
			log.Printf("link preview failed for %s: %v", msg.url, msg.err)
		}
		if m.windowManager.SetCachedLinkPreview(msg.chatGUID, msg.messageGUID, msg.preview) {
			m.saveMessageCache()
		}
		for _, window := range m.windowManager.WindowsShowingChat(msg.chatGUID) {
			window.Messages.SetLinkPreview(msg.messageGUID, msg.preview)
		}
		return m, nil

	case deleteChatSuccessMsg:
		m.removeChatLocalState(msg.chatGUID)
		m.saveMessageCache()
		m.saveLayoutState()
		return m, showToastCmd("Chat deleted", 3*time.Second)

	case deleteChatErrMsg:
		m.setAppError(msg.err)
		return m, nil

	case renameChatSuccessMsg:
		m.applyLocalChatAlias(msg.chatGUID, msg.displayName)
		return m, showToastCmd("Chat renamed", 3*time.Second)

	case renameChatErrMsg:
		m.applyLocalChatAlias(msg.chatGUID, msg.displayName)
		m.setAppError(fmt.Errorf("server rename failed; saved local alias: %w", msg.err))
		return m, nil

	case wsConnectSuccessMsg:
		m.wsConnected = true
		return m, tea.Batch(
			waitForWSEventCmd(m.wsClient),
			waitForWSReconnectCmd(m.wsClient),
			waitForWSDisconnectCmd(m.wsClient),
			waitForWSOverflowCmd(m.wsClient),
		)

	case wsDisconnectedMsg:
		m.wsConnected = false
		return m, waitForWSDisconnectCmd(m.wsClient)

	case wsOverflowMsg:
		// Events buffer overflowed - at least one event was dropped.
		// Resync every known chat so the missed message surfaces.
		cmds := []tea.Cmd{waitForWSOverflowCmd(m.wsClient)}
		if cmd := m.resyncAllChatsCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case wsReconnectedMsg:
		// Socket came back after a drop; any events that arrived while we
		// were offline are lost. Refetch all chats we know about so the
		// user never misses incoming messages silently.
		m.wsConnected = true
		cmds := []tea.Cmd{waitForWSReconnectCmd(m.wsClient)}
		if cmd := m.resyncAllChatsCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case wsConnectFailMsg:
		m.wsConnected = false
		m.setAppError(msg.err)
		return m, nil

	case markReadSuccessMsg:
		m.readMarkInFlight[msg.chatGUID] = false
		return m, nil

	case markReadErrMsg:
		m.readMarkInFlight[msg.chatGUID] = false
		low := strings.ToLower(msg.err.Error())
		if strings.Contains(low, "status 404") || strings.Contains(low, "private api") || strings.Contains(low, "status 501") {
			m.disableReadSync = true
			log.Printf("disabling remote read sync: %v", msg.err)
			return m, nil
		}
		log.Printf("mark chat read failed for %s: %v", msg.chatGUID, msg.err)
		return m, nil

	case refreshTickMsg:
		m.lastRefreshTime = time.Now()
		cmds := []tea.Cmd{refreshTickCmd(m.pollInterval)}
		if cmd := m.refreshOpenChatsCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case wsEventMsg:
		return m.handleWSEvent(models.WSEvent(msg))

	case errMsg:
		m.setAppError(msg.err)
		return m, nil

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

	case tea.KeyMsg:
		if m.showHelp {
			switch msg.String() {
			case "?", "esc", "q", "ctrl+c":
				m.showHelp = false
			}
			return m, nil
		}
		if m.chatAction != chatActionNone {
			return m.handleChatActionKey(msg)
		}

		m.lastKey = msg.String()
		if m.focused == focusChatList && !m.chatList.SearchActive() {
			switch msg.String() {
			case "d":
				if m.beginDeleteChatAction() {
					return m, nil
				}
			case "r":
				if m.beginRenameChatAction() {
					return m, nil
				}
			}
		}
		if m.focused == focusWindow {
			switch msg.String() {
			case "ctrl+d":
				if m.beginDeleteChatAction() {
					return m, nil
				}
			case "ctrl+r":
				if m.beginRenameChatAction() {
					return m, nil
				}
			case "up":
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirUp)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					cmd := m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
					return m, cmd
				}
				return m, nil
			case "down":
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirDown)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					cmd := m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
					return m, cmd
				}
				return m, nil
			}
		}

		// Handle global keys first
		switch msg.String() {
		case "?":
			m.showHelp = !m.showHelp
			return m, nil

		case "pgup":
			if m.focused == focusWindow {
				m.scrollFocusedMessages(-1)
			}
			return m, nil

		case "pgdown":
			if m.focused == focusWindow {
				m.scrollFocusedMessages(1)
			}
			return m, nil

		case "end":
			if m.focused == focusWindow {
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Messages.GotoBottom()
				}
			}
			return m, nil

		case "q", "ctrl+c":
			m.saveMessageCache()
			m.saveLayoutState()
			// Block briefly to make sure debounced writes hit disk before
			// we exit. This is the only intentional sync-to-disk point.
			m.persist.flushAll()
			return m, tea.Quit

		// Split operations
		case "ctrl+f":
			if !m.windowManager.SplitWindow(SplitHorizontal) {
				return m, showToastCmd("Max 4 panes (Ctrl+W to close)", 3*time.Second)
			}
			m.updateLayout()
			m.saveLayoutState()
			return m, nil

		case "ctrl+g":
			if !m.windowManager.SplitWindow(SplitVertical) {
				return m, showToastCmd("Max 4 panes (Ctrl+W to close)", 3*time.Second)
			}
			m.updateLayout()
			m.saveLayoutState()
			return m, nil

		case "ctrl+w":
			// Close focused window
			m.windowManager.CloseWindow()
			cmd := m.clearFocusedWindowNewMessageIndicator()
			m.updateLayout()
			m.saveLayoutState()
			return m, cmd

		case "ctrl+s":
			// Toggle chat list visibility
			m.showChatList = !m.showChatList
			if !m.showChatList && m.focused == focusChatList {
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				cmd := m.clearFocusedWindowNewMessageIndicator()
				m.updateLayout()
				m.saveUIState()
				return m, cmd
			}
			m.updateLayout()
			m.saveUIState()
			return m, nil

		case "ctrl+t":
			// Toggle timestamps
			m.showTimestamps = !m.showTimestamps
			m.windowManager.SetShowTimestamps(m.showTimestamps)
			m.saveUIState()
			return m, nil

		case "ctrl+n":
			// Toggle line numbers
			m.showLineNumbers = !m.showLineNumbers
			m.windowManager.SetShowLineNumbers(m.showLineNumbers)
			m.saveUIState()
			return m, nil

		case "ctrl+b", "alt+m", "ctrl+m":
			// Toggle sender names in message rows.
			// ctrl+m is kept for terminals that can distinguish it from Enter.
			// When disabled, rows show only IN/OUT direction + text.
			m.showSenderNames = !m.showSenderNames
			m.windowManager.SetShowSenderNames(m.showSenderNames)
			m.saveUIState()
			return m, nil

		case "ctrl+e":
			// Toggle pane dividers between split windows.
			m.showPaneDividers = !m.showPaneDividers
			m.windowManager.SetShowPaneDividers(m.showPaneDividers)
			m.saveUIState()
			return m, nil

		case "ctrl+p":
			m.showChatPreview = !m.showChatPreview
			m.chatList.SetShowPreview(m.showChatPreview)
			m.saveUIState()
			return m, nil

		case "escape":
			// Always go to chat list from a window
			if m.focused == focusWindow && m.showChatList {
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Blur()
				}
				m.focused = focusChatList
			}
			return m, nil

		// Arrow keys navigate between panes
		case "left":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirLeft)
				after := m.windowManager.FocusedWindow()
				if before == after {
					// No window to the left — go to chat list
					if m.showChatList {
						if window := m.windowManager.FocusedWindow(); window != nil {
							window.Input.textarea.Blur()
						}
						m.focused = focusChatList
					}
				} else {
					after.Input.textarea.Focus()
					cmd := m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
					return m, cmd
				}
			} else {
				// From chat list → go to focused window
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				cmd := m.clearFocusedWindowNewMessageIndicator()
				m.saveLayoutState()
				return m, cmd
			}
			return m, nil

		case "right":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirRight)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					cmd := m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
					return m, cmd
				}
			} else {
				// From chat list → go to focused window
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				cmd := m.clearFocusedWindowNewMessageIndicator()
				m.saveLayoutState()
				return m, cmd
			}
			return m, nil

		case "ctrl+shift+left":
			if m.focused == focusWindow {
				if m.windowManager.AdjustFocusedSplit(-0.05) {
					m.saveLayoutState()
				}
			}
			return m, nil

		case "ctrl+shift+right":
			if m.focused == focusWindow {
				if m.windowManager.AdjustFocusedSplit(0.05) {
					m.saveLayoutState()
				}
			}
			return m, nil

		case "ctrl+left":
			if m.showChatList {
				m.chatListWidth = clampChatListWidth(m.chatListWidth - ChatListResizeStep)
				m.updateLayout()
				m.saveUIState()
			}
			return m, nil

		case "ctrl+right":
			if m.showChatList {
				m.chatListWidth = clampChatListWidth(m.chatListWidth + ChatListResizeStep)
				m.updateLayout()
				m.saveUIState()
			}
			return m, nil

		case "ctrl+up":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirUp)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					cmd := m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
					return m, cmd
				}
			}
			return m, nil

		case "ctrl+down":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirDown)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					cmd := m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
					return m, cmd
				}
			}
			return m, nil

		case "tab":
			// Simple toggle: chat list ↔ currently focused window.
			// Arrow keys handle moving between windows.
			if m.focused == focusChatList {
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				cmd := m.clearFocusedWindowNewMessageIndicator()
				m.saveLayoutState()
				return m, cmd
			} else {
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Blur()
				}
				if m.showChatList {
					m.focused = focusChatList
				}
			}
			return m, nil

		case "enter":
			if m.focused == focusChatList && m.chatList.SearchActive() {
				m.chatList.ClearSearch()
				return m, nil
			}
			if m.focused == focusChatList {
				// Select chat and load in focused window
				selected := m.chatList.SelectedChat()
				if selected != nil {
					window := m.windowManager.FocusedWindow()
					if window != nil {
						// Switch focus to window input
						m.focused = focusWindow
						window.Input.textarea.Focus()
						return m, m.selectChat(selected, window)
					}
				}
				return m, nil
			} else if m.focused == focusWindow {
				// Send message from focused window
				window := m.windowManager.FocusedWindow()
				if window != nil && window.Chat != nil {
					text := window.Input.GetText()

					if cmd, handled := m.handleLocalInputCommand(window, text); handled {
						return m, cmd
					}

					if strings.TrimSpace(text) != "" {
						// Clear immediately to avoid visible input lag while waiting for
						// network confirmation.
						window.Input.Clear()
						pending := m.addPendingOutgoing(window.Chat.GUID, text)
						return m, tea.Batch(
							sendMessageCmd(m.apiClient, window.Chat.GUID, text, window.ID, pending.GUID),
							pendingTimeoutCmd(window.Chat.GUID, pending.GUID),
						)
					}
				}
				return m, nil
			}
			return m, nil
		}

		if m.focused == focusChatList && msg.String() == "/" {
			m.chatList.StartSearch()
			return m, nil
		}

		if m.focused == focusWindow && msg.String() == "G" {
			if window := m.windowManager.FocusedWindow(); window != nil {
				window.Messages.GotoBottom()
			}
			return m, nil
		}
	}

	// Delegate to focused component
	var cmd tea.Cmd
	switch m.focused {
	case focusChatList:
		m.chatList, cmd = m.chatList.Update(msg)
	case focusWindow:
		if window := m.windowManager.FocusedWindow(); window != nil {
			cmd = window.Update(msg)
		}
	}

	return m, cmd
}

func (m *AppModel) saveUIState() {
	m.persist.saveUI(config.UIState{
		ShowTimestamps:   m.showTimestamps,
		ShowLineNumbers:  m.showLineNumbers,
		ShowChatList:     m.showChatList,
		ShowSenderNames:  m.showSenderNames,
		ShowPaneDividers: m.showPaneDividers,
		ShowChatPreview:  m.showChatPreview,
		ChatListWidth:    m.chatListWidth,
	})
}

func clampChatListWidth(w int) int {
	if w <= 0 {
		return DefaultChatListWidth
	}
	if w < MinChatListWidth {
		return MinChatListWidth
	}
	if w > MaxChatListWidth {
		return MaxChatListWidth
	}
	return w
}

func (m *AppModel) currentActionChat() *models.Chat {
	if m.focused == focusChatList {
		return m.chatList.SelectedChat()
	}
	if window := m.windowManager.FocusedWindow(); window != nil {
		return window.Chat
	}
	return nil
}

func (m *AppModel) beginDeleteChatAction() bool {
	chat := m.currentActionChat()
	if chat == nil || strings.TrimSpace(chat.GUID) == "" {
		return false
	}
	m.chatAction = chatActionDelete
	m.actionChatGUID = chat.GUID
	m.actionChatName = chat.GetDisplayName()
	m.renameText = ""
	return true
}

func (m *AppModel) beginRenameChatAction() bool {
	chat := m.currentActionChat()
	if chat == nil || strings.TrimSpace(chat.GUID) == "" {
		return false
	}
	m.chatAction = chatActionRename
	m.actionChatGUID = chat.GUID
	m.actionChatName = chat.GetDisplayName()
	m.renameText = chat.GetDisplayName()
	return true
}

func (m *AppModel) clearChatAction() {
	m.chatAction = chatActionNone
	m.actionChatGUID = ""
	m.actionChatName = ""
	m.renameText = ""
}

func (m *AppModel) removeChatLocalState(chatGUID string) {
	chatGUID = strings.TrimSpace(chatGUID)
	if chatGUID == "" {
		return
	}
	m.chatList.RemoveChatByGUID(chatGUID)
	m.windowManager.DeleteCachedMessages(chatGUID)
	m.windowManager.ClearChatFromWindows(chatGUID)
	delete(m.messageFetchedAt, chatGUID)
	delete(m.messageFetchInFlight, chatGUID)
	delete(m.pendingOutgoing, chatGUID)
	delete(m.readMarkInFlight, chatGUID)
	delete(m.chatOverrides.Aliases, chatGUID)
	_ = config.SaveChatOverrides(m.chatOverrides)
}

func (m *AppModel) applyLocalChatAlias(chatGUID, displayName string) {
	chatGUID = strings.TrimSpace(chatGUID)
	displayName = strings.TrimSpace(displayName)
	if chatGUID == "" {
		return
	}
	if m.chatOverrides.Aliases == nil {
		m.chatOverrides.Aliases = make(map[string]string)
	}
	if displayName == "" {
		delete(m.chatOverrides.Aliases, chatGUID)
	} else {
		m.chatOverrides.Aliases[chatGUID] = displayName
	}
	m.chatList.SetLocalDisplayName(chatGUID, displayName)
	m.windowManager.SetLocalDisplayName(chatGUID, displayName)
	_ = config.SaveChatOverrides(m.chatOverrides)
}

func (m AppModel) handleChatActionKey(msg tea.KeyMsg) (AppModel, tea.Cmd) {
	switch m.chatAction {
	case chatActionDelete:
		switch msg.String() {
		case "esc", "escape":
			m.clearChatAction()
			return m, nil
		case "D":
			chatGUID := m.actionChatGUID
			m.clearChatAction()
			return m, deleteChatCmd(m.apiClient, chatGUID)
		default:
			return m, nil
		}
	case chatActionRename:
		switch msg.String() {
		case "esc", "escape":
			m.clearChatAction()
			return m, nil
		case "backspace", "ctrl+h":
			if m.renameText != "" {
				runes := []rune(m.renameText)
				m.renameText = string(runes[:len(runes)-1])
			}
			return m, nil
		case "enter":
			chatGUID := m.actionChatGUID
			displayName := strings.TrimSpace(m.renameText)
			m.clearChatAction()
			return m, renameChatCmd(m.apiClient, chatGUID, displayName)
		default:
			if len(msg.Runes) > 0 {
				m.renameText += string(msg.Runes)
			}
			return m, nil
		}
	default:
		return m, nil
	}
}

func (m *AppModel) updateLayout() {
	contentHeight := m.height - 1 // reserve one row for status bar
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Calculate chat list dimensions (no borders, just padding)
	chatListContentHeight := contentHeight
	chatListWidth := 0
	if m.showChatList {
		chatListWidth = m.chatListWidth
	}
	m.chatList.SetSize(chatListWidth, chatListContentHeight)
	m.chatList.SetFocused(m.focused == focusChatList && m.showChatList)

	// Calculate window area (everything to the right of chat list)
	windowsWidth := m.width - 2 // -2 for padding
	if m.showChatList {
		windowsWidth -= m.chatListWidth
	}
	if windowsWidth < 1 {
		windowsWidth = 1
	}
	windowsHeight := contentHeight
	if windowsHeight < 1 {
		windowsHeight = 1
	}

	m.windowManager.SetSize(windowsWidth, windowsHeight)
}

func (m AppModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	if m.showHelp {
		return helpOverlayView(m.width, m.height)
	}

	// Render chat list panel. Reserve one row for the status bar so the
	// total frame height stays at m.height — otherwise the alt-screen
	// scrolls and the status row gets clipped off the top.
	chatPanel := ""
	if m.showChatList {
		chatListStyle := PanelStyle
		if m.focused == focusChatList {
			chatListStyle = ActivePanelStyle
		}
		panelHeight := m.height - 1
		if panelHeight < 1 {
			panelHeight = 1
		}
		chatPanel = chatListStyle.
			Width(m.chatListWidth).
			Height(panelHeight).
			MaxHeight(panelHeight).
			Render(m.chatList.View())
	}

	// Render windows area
	windowsView := m.windowManager.Render()

	// Join panels horizontally
	content := windowsView
	if m.showChatList {
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			chatPanel,
			windowsView,
		)
	}

	statusBar := m.statusBarView()
	return lipgloss.JoinVertical(lipgloss.Left, statusBar, content)
}

func (m AppModel) statusBarView() string {
	connColor := ColorConnectionDown
	if m.wsConnected {
		connColor = ColorConnectionUp
	}
	dot := lipgloss.NewStyle().Foreground(connColor).Bold(true).Render("●")
	parts := []string{dot}

	if m.loading {
		parts = append(parts, "loading chats…")
	}
	if actionText := m.chatActionStatusText(); actionText != "" {
		parts = append(parts, actionText)
	}

	if window := m.windowManager.FocusedWindow(); m.focused == focusWindow && window != nil && window.Chat != nil {
		if window.PaneTotal > 1 {
			parts = append(parts, fmt.Sprintf("pane %d/%d", window.PaneIndex, window.PaneTotal))
		}
	}

	newMessageCount := m.chatList.NewMessageCount()
	if !m.showChatList && newMessageCount > 0 {
		newDot := lipgloss.NewStyle().
			Foreground(ColorChatListNewMessage).
			Render("●")
		label := "new message"
		if newMessageCount > 1 {
			label = "new messages"
		}
		parts = append(parts, fmt.Sprintf("%s %d %s", newDot, newMessageCount, label))
	}

	now := time.Now()
	if m.err != nil && !m.errUntil.IsZero() && now.Before(m.errUntil) {
		errText := m.err.Error()
		if len(errText) > 48 {
			errText = errText[:47] + "…"
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(ColorChatListNewMessage).Render("! "+errText))
	}
	if m.toastText != "" && !m.toastUntil.IsZero() && now.Before(m.toastUntil) {
		parts = append(parts, m.toastText)
	}

	parts = append(parts, "? help")

	status := strings.Join(parts, "  ")
	return StatusBarStyle.
		Width(m.width).
		Render(status)
}

func (m AppModel) chatActionStatusText() string {
	switch m.chatAction {
	case chatActionDelete:
		return fmt.Sprintf("Delete %q? Press D to confirm, Esc to cancel", m.actionChatName)
	case chatActionRename:
		return fmt.Sprintf("Rename %q: %s", m.actionChatName, m.renameText)
	default:
		return ""
	}
}

// Command constructors

func loadChatsCmd(client *api.Client, limit int) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats(limit)
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load chats: %v", err)}
		}
		return chatsLoadedMsg(chats)
	}
}

func loadMessagesCmd(client *api.Client, chatGUID string, limit int) tea.Cmd {
	return func() tea.Msg {
		messages, err := client.GetMessages(chatGUID, limit)
		if err != nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("failed to load messages: %v", err), report: true}
		}
		return messagesLoadedMsg{chatGUID: chatGUID, messages: messages}
	}
}

func prefetchMessagesCmd(client *api.Client, chatGUID string, limit int) tea.Cmd {
	return func() tea.Msg {
		messages, err := client.GetMessages(chatGUID, limit)
		if err != nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("failed to prefetch messages: %v", err), report: false}
		}
		return messagesLoadedMsg{chatGUID: chatGUID, messages: messages}
	}
}

func (m *AppModel) selectChat(chat *models.Chat, window *ChatWindow) tea.Cmd {
	if chat == nil || window == nil {
		return nil
	}

	window.SetChat(chat)
	m.chatList.ClearNewMessage(chat.GUID)
	m.saveLayoutState()

	if cached := m.windowManager.GetCachedMessages(chat.GUID); len(cached) > 0 {
		window.Messages.SetMessages(cached)
	}

	var cmds []tea.Cmd
	if m.shouldRefreshMessages(chat.GUID) {
		m.messageFetchInFlight[chat.GUID] = true
		window.Messages.SetLoading(true)
		cmds = append(cmds, loadMessagesCmd(m.apiClient, chat.GUID, m.messageLimit))
	}
	if cmd := m.markChatReadIfNeeded(chat.GUID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *AppModel) shouldRefreshMessages(chatGUID string) bool {
	if m.messageFetchInFlight[chatGUID] {
		return false
	}

	cached := m.windowManager.GetCachedMessages(chatGUID)
	if len(cached) == 0 {
		return true
	}

	lastFetch := m.messageFetchedAt[chatGUID]
	if lastFetch.IsZero() {
		return true
	}

	return time.Since(lastFetch) > messageCacheRefreshTTL
}

func applyChatOverrides(chats []models.Chat, aliases map[string]string) {
	if len(chats) == 0 || len(aliases) == 0 {
		return
	}
	for i := range chats {
		alias := strings.TrimSpace(aliases[chats[i].GUID])
		if alias == "" {
			continue
		}
		chats[i].LocalDisplayName = alias
	}
}

func mergeLoadedMessagesWithCache(loaded, cached []models.Message) []models.Message {
	merged := make([]models.Message, len(loaded))
	copy(merged, loaded)

	cachedByKey := make(map[string]models.Message, len(cached)*2)
	for _, msg := range cached {
		for _, key := range messageDedupeKeys(msg) {
			cachedByKey[key] = msg
		}
	}

	for i := range merged {
		for _, key := range messageDedupeKeys(merged[i]) {
			cachedMsg, ok := cachedByKey[key]
			if !ok {
				continue
			}
			for _, preview := range cachedMsg.LinkPreviews {
				merged[i].LinkPreviews = upsertLinkPreview(merged[i].LinkPreviews, preview)
			}
			break
		}
	}

	if len(merged) == 0 {
		return merged
	}

	newestLoadedTime := merged[0].DateCreated
	for _, loadedMsg := range merged[1:] {
		if loadedMsg.DateCreated > newestLoadedTime {
			newestLoadedTime = loadedMsg.DateCreated
		}
	}
	for _, cachedMsg := range cached {
		if cachedMsg.DateCreated <= newestLoadedTime {
			continue
		}
		found := false
		for _, loadedMsg := range merged {
			if messagesShareDedupeKey(loadedMsg, cachedMsg) {
				found = true
				break
			}
		}
		if !found {
			merged = append(merged, cachedMsg)
		}
	}
	return merged
}

func messagesShareDedupeKey(a, b models.Message) bool {
	keys := make(map[string]struct{})
	for _, key := range messageDedupeKeys(a) {
		keys[key] = struct{}{}
	}
	for _, key := range messageDedupeKeys(b) {
		if _, ok := keys[key]; ok {
			return true
		}
	}
	return false
}

func (m *AppModel) restoreMessageCache() {
	state := config.LoadMessageCache()
	if len(state.Chats) == 0 {
		return
	}

	now := time.Now()
	for chatGUID, cached := range state.Chats {
		if strings.TrimSpace(chatGUID) == "" || len(cached.Messages) == 0 {
			continue
		}
		m.windowManager.SetCachedMessages(chatGUID, cached.Messages)

		// Treat restored cache as fresh at startup to avoid burst API fetching
		// for many chats immediately after launch.
		m.messageFetchedAt[chatGUID] = now
	}
}

func (m *AppModel) saveMessageCache() {
	snapshot := m.windowManager.CachedMessagesSnapshot()
	state := config.MessageCacheState{
		Chats: make(map[string]config.CachedChatMessages, len(snapshot)),
	}
	for chatGUID, messages := range snapshot {
		if strings.TrimSpace(chatGUID) == "" || len(messages) == 0 {
			continue
		}
		fetchedAt := time.Now().UnixMilli()
		if t, ok := m.messageFetchedAt[chatGUID]; ok && !t.IsZero() {
			fetchedAt = t.UnixMilli()
		}
		state.Chats[chatGUID] = config.CachedChatMessages{
			Messages:           messages,
			FetchedAtUnixMilli: fetchedAt,
		}
	}

	m.persist.saveMessages(state)
}

func (m *AppModel) restoreLayoutFromState(chats []models.Chat) bool {
	if m.didRestoreLayout {
		return false
	}
	m.didRestoreLayout = true

	if m.savedLayoutState == nil || m.savedLayoutState.Root == nil {
		return false
	}

	chatByGUID := make(map[string]*models.Chat, len(chats))
	for i := range chats {
		chatByGUID[chats[i].GUID] = &chats[i]
	}

	wm := m.windowManager
	wm.windows = make(map[WindowID]*ChatWindow)
	wm.root = nil
	wm.focusedWindow = 0
	wm.nextID = 0

	var leaves []*ChatWindow
	var build func(node *config.LayoutNodeState) *LayoutNode
	build = func(node *config.LayoutNodeState) *LayoutNode {
		if node == nil {
			return nil
		}

		dir := SplitDirection(node.Direction)
		if dir == SplitNone {
			id := wm.nextID
			wm.nextID++
			window := NewChatWindow(id)
			window.Messages.SetShowTimestamps(wm.showTimestamps)
			window.Messages.SetShowLineNumbers(wm.showLineNumbers)
			window.Messages.SetShowSenderNames(wm.showSenderNames)
			wm.windows[id] = window
			leaves = append(leaves, window)
			return NewLeafNode(window)
		}

		left := build(node.Left)
		right := build(node.Right)
		if left == nil || right == nil {
			return nil
		}
		split := NewSplitNode(dir, left, right)
		split.SplitRatio = node.SplitRatio
		return split
	}

	root := build(m.savedLayoutState.Root)
	if root == nil || len(leaves) == 0 || len(leaves) > wm.maxWindows {
		wm.windows = make(map[WindowID]*ChatWindow)
		window := NewChatWindow(0)
		window.Messages.SetShowTimestamps(wm.showTimestamps)
		window.Messages.SetShowLineNumbers(wm.showLineNumbers)
		window.Messages.SetShowSenderNames(wm.showSenderNames)
		window.Focused = true
		wm.windows[0] = window
		wm.focusedWindow = 0
		wm.root = NewLeafNode(window)
		wm.nextID = 1
		return false
	}

	for i, window := range leaves {
		if i >= len(m.savedLayoutState.LeafChatGUIDs) {
			continue
		}
		chatGUID := strings.TrimSpace(m.savedLayoutState.LeafChatGUIDs[i])
		chat, ok := chatByGUID[chatGUID]
		if !ok {
			continue
		}
		window.SetChat(chat)
		if cached := wm.GetCachedMessages(chatGUID); len(cached) > 0 {
			window.Messages.SetMessages(cached)
		}
	}

	for _, window := range leaves {
		window.Focused = false
	}
	focusIdx := m.savedLayoutState.FocusedLeafIndex
	if focusIdx < 0 || focusIdx >= len(leaves) {
		focusIdx = 0
	}
	leaves[focusIdx].Focused = true

	wm.root = root
	wm.focusedWindow = leaves[focusIdx].ID
	m.focused = focusWindow
	wm.recalculateLayout()
	m.clearFocusedWindowNewMessageIndicator()
	return true
}

func (m *AppModel) saveLayoutState() {
	wm := m.windowManager
	if wm == nil || wm.root == nil {
		return
	}

	leafOrder := make([]WindowID, 0, wm.root.CountWindows())
	var encode func(node *LayoutNode) *config.LayoutNodeState
	encode = func(node *LayoutNode) *config.LayoutNodeState {
		if node == nil {
			return nil
		}

		state := &config.LayoutNodeState{
			Direction:  int(node.Direction),
			SplitRatio: node.SplitRatio,
		}
		if node.Direction == SplitNone {
			if node.Window != nil {
				leafOrder = append(leafOrder, node.Window.ID)
			}
			return state
		}

		state.Left = encode(node.Left)
		state.Right = encode(node.Right)
		return state
	}

	root := encode(wm.root)
	if root == nil || len(leafOrder) == 0 {
		return
	}

	leafChatGUIDs := make([]string, len(leafOrder))
	focusedLeafIndex := 0
	for i, id := range leafOrder {
		window := wm.windows[id]
		if window != nil && window.Chat != nil {
			leafChatGUIDs[i] = window.Chat.GUID
		}
		if id == wm.focusedWindow {
			focusedLeafIndex = i
		}
	}

	state := config.LayoutState{
		Root:             root,
		LeafChatGUIDs:    leafChatGUIDs,
		FocusedLeafIndex: focusedLeafIndex,
	}
	m.persist.saveLayout(state)
}

func (m *AppModel) clearFocusedWindowNewMessageIndicator() tea.Cmd {
	if m.focused != focusWindow {
		return nil
	}
	window := m.windowManager.FocusedWindow()
	if window == nil || window.Chat == nil {
		return nil
	}
	m.chatList.SelectChatByGUID(window.Chat.GUID)
	m.chatList.ClearNewMessage(window.Chat.GUID)
	return m.markChatReadIfNeeded(window.Chat.GUID)
}

func (m *AppModel) markChatReadIfNeeded(chatGUID string) tea.Cmd {
	chatGUID = strings.TrimSpace(chatGUID)
	if chatGUID == "" || m.disableReadSync || m.readMarkInFlight[chatGUID] {
		return nil
	}
	m.readMarkInFlight[chatGUID] = true
	return markChatReadCmd(m.apiClient, chatGUID)
}

func (m *AppModel) prefetchTopChatsCmd(chats []models.Chat, skipGUID string) tea.Cmd {
	if len(chats) == 0 {
		return nil
	}

	limit := initialPrefetchChatCount
	if len(chats) < limit {
		limit = len(chats)
	}

	cmds := make([]tea.Cmd, 0, limit)
	for i := 0; i < limit; i++ {
		chatGUID := chats[i].GUID
		if chatGUID == "" || chatGUID == skipGUID {
			continue
		}
		if !m.shouldRefreshMessages(chatGUID) {
			continue
		}
		m.messageFetchInFlight[chatGUID] = true
		cmds = append(cmds, prefetchMessagesCmd(m.apiClient, chatGUID, m.messageLimit))
	}

	if len(cmds) == 0 {
		return nil
	}

	return tea.Batch(cmds...)
}

func sendMessageCmd(client *api.Client, chatGUID, text string, windowID WindowID, pendingGUID string) tea.Cmd {
	return func() tea.Msg {
		if err := client.SendMessageWithTempGUID(chatGUID, text, "", pendingGUID); err != nil {
			return sendErrMsg{err: err, chatGUID: chatGUID, pendingGUID: pendingGUID}
		}
		return sendSuccessMsg{windowID: windowID}
	}
}

func markChatReadCmd(client *api.Client, chatGUID string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return markReadErrMsg{chatGUID: chatGUID, err: fmt.Errorf("api client not configured")}
		}
		if err := client.MarkChatRead(chatGUID); err != nil {
			return markReadErrMsg{chatGUID: chatGUID, err: err}
		}
		return markReadSuccessMsg{chatGUID: chatGUID}
	}
}

func deleteChatCmd(client *api.Client, chatGUID string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return deleteChatErrMsg{chatGUID: chatGUID, err: fmt.Errorf("api client not configured")}
		}
		if err := client.DeleteChat(chatGUID); err != nil {
			return deleteChatErrMsg{chatGUID: chatGUID, err: err}
		}
		return deleteChatSuccessMsg{chatGUID: chatGUID}
	}
}

func renameChatCmd(client *api.Client, chatGUID, displayName string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return renameChatErrMsg{chatGUID: chatGUID, displayName: displayName, err: fmt.Errorf("api client not configured")}
		}
		if err := client.RenameChat(chatGUID, displayName); err != nil {
			return renameChatErrMsg{chatGUID: chatGUID, displayName: displayName, err: err}
		}
		return renameChatSuccessMsg{chatGUID: chatGUID, displayName: displayName}
	}
}

func (m *AppModel) addPendingOutgoing(chatGUID, text string) models.Message {
	pending := models.Message{
		GUID:        uuid.New().String(),
		Text:        text,
		IsFromMe:    true,
		DateCreated: time.Now().UnixMilli(),
		ChatGUID:    chatGUID,
		Pending:     true,
	}

	m.pendingOutgoing[chatGUID] = append(m.pendingOutgoing[chatGUID], pending)
	m.chatList.UpdateChatPreview(chatGUID, text, pending.DateCreated)
	for _, window := range m.windowManager.WindowsShowingChat(chatGUID) {
		window.Messages.AppendMessage(pending)
	}
	return pending
}

// pendingTimeoutCmd fires after pendingOutgoingTimeout. If the optimistic
// message is still in the pending map by then, we assume the server echo
// was lost and drop it before refetching the chat.
func pendingTimeoutCmd(chatGUID, pendingGUID string) tea.Cmd {
	return tea.Tick(pendingOutgoingTimeout, func(time.Time) tea.Msg {
		return pendingTimeoutMsg{chatGUID: chatGUID, pendingGUID: pendingGUID}
	})
}

func (m *AppModel) hasPendingOutgoing(chatGUID, guid string) bool {
	for _, p := range m.pendingOutgoing[chatGUID] {
		if p.GUID == guid {
			return true
		}
	}
	return false
}

func (m *AppModel) removePendingOutgoingByGUID(chatGUID, guid string) bool {
	if strings.TrimSpace(chatGUID) == "" || strings.TrimSpace(guid) == "" {
		return false
	}
	pending := m.pendingOutgoing[chatGUID]
	for i, msg := range pending {
		if msg.GUID != guid {
			continue
		}
		m.pendingOutgoing[chatGUID] = append(pending[:i], pending[i+1:]...)
		if len(m.pendingOutgoing[chatGUID]) == 0 {
			delete(m.pendingOutgoing, chatGUID)
		}
		for _, window := range m.windowManager.WindowsShowingChat(chatGUID) {
			window.Messages.RemoveMessageByGUID(guid)
		}
		return true
	}
	return false
}

func (m *AppModel) matchAndRemovePendingOutgoing(msg models.Message) {
	if !msg.IsFromMe || strings.TrimSpace(msg.ChatGUID) == "" {
		return
	}

	pending := m.pendingOutgoing[msg.ChatGUID]
	if len(pending) == 0 {
		return
	}

	body := strings.TrimSpace(msg.Text)
	matchIdx := -1
	var matchTime int64
	for i, p := range pending {
		if strings.TrimSpace(p.Text) != body {
			continue
		}
		// Guard against accidental match to old pending entry.
		diff := msg.DateCreated - p.DateCreated
		if diff < 0 {
			diff = -diff
		}
		if diff > 2*60*1000 {
			continue
		}
		// Prefer the OLDEST matching pending entry so that sending the same
		// text twice in a row pairs the first server echo with the first
		// local entry instead of collapsing them.
		if matchIdx < 0 || p.DateCreated < matchTime {
			matchIdx = i
			matchTime = p.DateCreated
		}
	}
	if matchIdx < 0 {
		return
	}
	_ = m.removePendingOutgoingByGUID(msg.ChatGUID, pending[matchIdx].GUID)
}

func (m *AppModel) handleLocalInputCommand(window *ChatWindow, raw string) (tea.Cmd, bool) {
	msgNum, handled, err := parseImgCommand(raw)
	if !handled {
		return nil, false
	}
	if err != nil {
		m.setAppError(err)
		return nil, true
	}

	att, ok := window.Messages.FirstImageAttachmentByNumber(msgNum)
	if !ok {
		m.setAppError(fmt.Errorf("message #%d has no image attachment", msgNum))
		return nil, true
	}

	window.Input.Clear()
	return openImageAttachmentCmd(m.apiClient, att), true
}

func parseImgCommand(raw string) (int, bool, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "/img") {
		return 0, false, nil
	}
	parts := strings.Fields(s)
	if len(parts) > 0 && parts[0] != "/img" {
		return 0, false, nil
	}
	if len(parts) != 2 {
		return 0, true, fmt.Errorf("usage: /img #<message-number>")
	}
	nRaw := strings.TrimPrefix(parts[1], "#")
	n, err := strconv.Atoi(nRaw)
	if err != nil || n < 1 {
		return 0, true, fmt.Errorf("invalid message number: %s", parts[1])
	}
	return n, true, nil
}

func openImageAttachmentCmd(client *api.Client, att models.Attachment) tea.Cmd {
	return func() tea.Msg {
		target := attachmentOpenTarget(att)
		if target == "" {
			if client == nil || strings.TrimSpace(att.GUID) == "" {
				return errMsg{err: fmt.Errorf("image has no openable target")}
			}
			path, err := downloadAttachmentToTemp(client, att)
			if err != nil {
				return errMsg{err: fmt.Errorf("failed to download image: %v", err)}
			}
			target = path
		}
		if err := openWithSystem(target); err != nil {
			return errMsg{err: fmt.Errorf("failed to open image: %v", err)}
		}
		return nil
	}
}

func attachmentOpenTarget(att models.Attachment) string {
	for _, raw := range []string{att.URL, att.PathOnDisk, att.Path} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "file://") {
			return raw
		}
		if strings.HasPrefix(raw, "/") {
			if _, err := os.Stat(raw); err == nil {
				return raw
			}
		}
	}
	return ""
}

func downloadAttachmentToTemp(client *api.Client, att models.Attachment) (string, error) {
	data, mimeType, err := client.DownloadAttachment(att.GUID)
	if err != nil {
		return "", err
	}

	ext := strings.TrimSpace(filepath.Ext(att.FileName))
	if ext == "" {
		mt := strings.TrimSpace(strings.Split(mimeType, ";")[0])
		if mt != "" {
			if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
				ext = exts[0]
			}
		}
	}
	if ext == "" {
		ext = ".img"
	}

	f, err := os.CreateTemp("", "bluebubbles-img-*"+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func openWithSystem(target string) error {
	name, args := openCommand(target)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func openURLCmd(rawURL string) tea.Cmd {
	return func() tea.Msg {
		if err := openWithSystem(rawURL); err != nil {
			return errMsg{err: fmt.Errorf("failed to open link: %v", err)}
		}
		return nil
	}
}

func openCommand(target string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{target}
	case "windows":
		return "cmd", []string{"/c", "start", "", target}
	default:
		return "xdg-open", []string{target}
	}
}

func connectWSCmd(wsClient *ws.Client) tea.Cmd {
	return func() tea.Msg {
		if err := wsClient.Connect(); err != nil {
			return wsConnectFailMsg{err: fmt.Errorf("websocket connection failed: %v", err)}
		}
		return wsConnectSuccessMsg{}
	}
}

func waitForWSEventCmd(wsClient *ws.Client) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-wsClient.Events
		if !ok {
			return errMsg{err: fmt.Errorf("websocket connection closed")}
		}
		return wsEventMsg(event)
	}
}

func waitForWSReconnectCmd(wsClient *ws.Client) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-wsClient.Reconnect
		if !ok {
			return nil
		}
		return wsReconnectedMsg{}
	}
}

func waitForWSDisconnectCmd(wsClient *ws.Client) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-wsClient.Disconnect
		if !ok {
			return nil
		}
		return wsDisconnectedMsg{}
	}
}

func waitForWSOverflowCmd(wsClient *ws.Client) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-wsClient.Overflow
		if !ok {
			return nil
		}
		return wsOverflowMsg{}
	}
}

func refreshTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

// refreshOpenChatsCmd periodically reconciles every chat currently visible in a
// pane. WebSocket remains the fast path; this catches missed/delayed events and
// keeps panes fresh when the socket is reconnecting.
func (m *AppModel) refreshOpenChatsCmd() tea.Cmd {
	seen := make(map[string]struct{})
	var cmds []tea.Cmd

	for _, window := range m.windowManager.AllWindows() {
		if window.Chat == nil {
			continue
		}
		chatGUID := strings.TrimSpace(window.Chat.GUID)
		if chatGUID == "" {
			continue
		}
		if _, ok := seen[chatGUID]; ok {
			continue
		}
		seen[chatGUID] = struct{}{}
		if m.messageFetchInFlight[chatGUID] {
			continue
		}
		m.messageFetchInFlight[chatGUID] = true
		cmds = append(cmds, prefetchMessagesCmd(m.apiClient, chatGUID, m.messageLimit))
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// resyncAllChatsCmd forces a fresh API fetch for every chat we already know
// about (open in a pane or cached). Used after WS reconnect and after an
// events-buffer overflow so that missed messages surface automatically.
func (m *AppModel) resyncAllChatsCmd() tea.Cmd {
	seen := make(map[string]struct{})
	var cmds []tea.Cmd

	add := func(chatGUID string) {
		chatGUID = strings.TrimSpace(chatGUID)
		if chatGUID == "" {
			return
		}
		if _, ok := seen[chatGUID]; ok {
			return
		}
		seen[chatGUID] = struct{}{}
		if m.messageFetchInFlight[chatGUID] {
			return
		}
		// Force through the TTL gate - this is a resync, not a cache read.
		m.messageFetchInFlight[chatGUID] = true
		cmds = append(cmds, prefetchMessagesCmd(m.apiClient, chatGUID, m.messageLimit))
	}

	for _, window := range m.windowManager.AllWindows() {
		if window.Chat != nil {
			add(window.Chat.GUID)
		}
	}
	for chatGUID := range m.windowManager.CachedMessagesSnapshot() {
		add(chatGUID)
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// handleWSEvent processes incoming WebSocket events
func (m *AppModel) handleWSEvent(event models.WSEvent) (tea.Model, tea.Cmd) {
	log.Printf("[WS-DEBUG] handleWSEvent type=%q", event.Type)
	switch event.Type {
	case "new-message":
		// Parse incoming message
		var wsMsg struct {
			models.Message
			ChatGUID string `json:"chatGuid"`
			Chats    []struct {
				GUID string `json:"guid"`
			} `json:"chats"`
		}
		if err := json.Unmarshal(event.Data, &wsMsg); err != nil {
			log.Printf("[WS-DEBUG] new-message unmarshal failed: %v", err)
			return m, waitForWSEventCmd(m.wsClient)
		}

		msg := wsMsg.Message
		if len(wsMsg.Chats) > 0 {
			msg.ChatGUID = wsMsg.Chats[0].GUID
		} else if msg.ChatGUID == "" {
			msg.ChatGUID = wsMsg.ChatGUID
		}
		log.Printf("[WS-DEBUG] new-message guid=%q chatGUID=%q text=%q fromMe=%v",
			msg.GUID, msg.ChatGUID, msg.Text, msg.IsFromMe)

		if msg.ChatGUID != "" {
			if len(messageDedupeKeys(msg)) == 0 {
				log.Printf("ignoring websocket new-message without usable identity for chat %s", msg.ChatGUID)
				return m, waitForWSEventCmd(m.wsClient)
			}

			// Replace a local optimistic echo with the server-confirmed message.
			m.matchAndRemovePendingOutgoing(msg)

			// Cache the message (ignore duplicate WS events by message identity).
			if !m.windowManager.CacheMessage(msg.ChatGUID, msg) {
				log.Printf("[WS-DEBUG] CacheMessage rejected as duplicate: guid=%q", msg.GUID)
				return m, waitForWSEventCmd(m.wsClient)
			}
			log.Printf("[WS-DEBUG] CacheMessage accepted, %d windows show this chat",
				len(m.windowManager.WindowsShowingChat(msg.ChatGUID)))
			// Intentionally do NOT refresh messageFetchedAt here. Only real
			// API fetches bump it so the TTL in shouldRefreshMessages can
			// still trigger a reconciling refetch and heal any hole that
			// WS may have left behind.
			m.saveMessageCache()

			// Update ALL windows showing this chat
			windowsShowing := m.windowManager.WindowsShowingChat(msg.ChatGUID)
			for _, window := range windowsShowing {
				window.Messages.AppendMessage(msg)
				if !msg.IsFromMe && !window.Focused {
					window.Messages.MarkIncomingUnseen(msg.GUID)
				}
			}

			// Local read/unread policy:
			// - If incoming message is for the currently focused window, clear indicator.
			// - If message is from us, don't create "new message" indicator.
			// - Otherwise keep/mark red indicator until user focuses that pane.
			if focused := m.windowManager.FocusedWindow(); !msg.IsFromMe &&
				(m.focused == focusWindow && focused != nil && focused.Chat != nil && focused.Chat.GUID == msg.ChatGUID) {
				m.chatList.ClearNewMessage(msg.ChatGUID)
			} else if !msg.IsFromMe {
				m.chatList.MarkNewMessage(msg.ChatGUID)
			}
			preview := strings.TrimSpace(msg.Text)
			if preview == "" && len(msg.Attachments) > 0 {
				preview = attachmentPreviewFromMessage(msg)
			}
			m.chatList.UpdateChatPreview(msg.ChatGUID, preview, msg.DateCreated)
		}

		cmds := []tea.Cmd{waitForWSEventCmd(m.wsClient)}
		if cmd := m.linkPreviewCmdsForMessages(msg.ChatGUID, []models.Message{msg}); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case "updated-message":
		return m, waitForWSEventCmd(m.wsClient)

	case "chat-read-status-changed":
		return m, waitForWSEventCmd(m.wsClient)

	default:
		return m, waitForWSEventCmd(m.wsClient)
	}
}

func (m *AppModel) setAppError(err error) {
	if err == nil {
		return
	}
	m.err = err
	m.errUntil = time.Now().Add(5 * time.Second)
}

func (m AppModel) scrollFocusedMessages(direction int) {
	window := m.windowManager.FocusedWindow()
	if window == nil {
		return
	}
	if direction < 0 {
		window.Messages.ScrollPageUp()
	} else {
		window.Messages.ScrollPageDown()
	}
}

func (m AppModel) handleMouseMsg(msg tea.MouseMsg) (AppModel, tea.Cmd) {
	contentY := msg.Y - 1
	if contentY < 0 {
		return m, nil
	}

	if mouseIsWheel(msg) {
		relX := msg.X
		if m.showChatList {
			if msg.X < m.chatListWidth {
				return m, nil
			}
			relX = msg.X - m.chatListWidth
		}
		if window := m.windowManager.WindowAt(relX, contentY); window != nil {
			if msg.Button == tea.MouseButtonWheelUp {
				window.Messages.ScrollUp()
			} else if msg.Button == tea.MouseButtonWheelDown {
				window.Messages.ScrollDown()
			}
		}
		return m, nil
	}

	if m.dividerDragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			relX := msg.X
			if m.showChatList {
				relX = msg.X - m.chatListWidth
			}
			if m.windowManager.SetSplitRatioFromPoint(m.dividerNode, relX, contentY) {
				m.updateLayout()
			}
			return m, nil
		case tea.MouseActionRelease:
			m.dividerDragging = false
			m.dividerNode = nil
			m.saveLayoutState()
			return m, nil
		}
	}

	if m.showChatList && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		border := m.chatListWidth
		if msg.X >= border-1 && msg.X <= border+1 {
			m.sidebarDragging = true
			m.sidebarDragStart = msg.X
			return m, nil
		}
	}

	if m.sidebarDragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			if m.showChatList {
				delta := msg.X - m.sidebarDragStart
				if delta != 0 {
					m.chatListWidth = clampChatListWidth(m.chatListWidth + delta)
					m.sidebarDragStart = msg.X
					m.updateLayout()
				}
			}
			return m, nil
		case tea.MouseActionRelease:
			m.sidebarDragging = false
			m.saveUIState()
			return m, nil
		}
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if m.showChatList && msg.X < m.chatListWidth {
			if m.focused == focusWindow {
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Blur()
				}
			}
			m.focused = focusChatList
			m.updateLayout()
			m.chatList.ClickAt(contentY)
			return m, nil
		}

		relX := msg.X
		if m.showChatList {
			relX = msg.X - m.chatListWidth
		}
		if divider := m.windowManager.DividerAt(relX, contentY); divider != nil {
			m.dividerDragging = true
			m.dividerNode = divider
			return m, nil
		}
		if window := m.windowManager.WindowAt(relX, contentY); window != nil {
			localY := contentY - window.y
			if old := m.windowManager.FocusedWindow(); old != nil && old.ID != window.ID {
				old.Input.textarea.Blur()
			}
			m.windowManager.SetFocus(window.ID)
			window.Input.textarea.Focus()
			m.focused = focusWindow
			cmd := m.clearFocusedWindowNewMessageIndicator()
			m.saveLayoutState()
			if att, ok := window.FirstImageAttachmentAtContentY(localY); ok {
				return m, tea.Batch(cmd, openImageAttachmentCmd(m.apiClient, att))
			}
			if rawURL, ok := window.LinkAtContentY(localY); ok {
				return m, tea.Batch(cmd, openURLCmd(rawURL))
			}
			return m, cmd
		}
	}

	return m, nil
}

func attachmentPreviewFromMessage(msg models.Message) string {
	if len(msg.Attachments) == 0 {
		return ""
	}
	if label := attachmentLabel(msg, false); label != "" {
		return label
	}
	return "[attachment]"
}

func noticeExpireCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return noticeExpireMsg{}
	})
}

func showToastCmd(text string, duration time.Duration) tea.Cmd {
	return func() tea.Msg {
		return toastMsg{text: text, duration: duration}
	}
}

func (m *AppModel) linkPreviewCmdsForMessages(chatGUID string, messages []models.Message) tea.Cmd {
	if !m.enableLinkPreviews || m.apiClient == nil || m.maxPreviewsPerMessage <= 0 {
		return nil
	}

	var cmds []tea.Cmd
	for _, msg := range messages {
		messageGUID := strings.TrimSpace(msg.GUID)
		if messageGUID == "" {
			continue
		}
		for _, rawURL := range supportedMediaLinksFromText(msg.Text, m.maxPreviewsPerMessage) {
			if messageHasPreviewAttempt(msg, rawURL) {
				continue
			}
			key := linkPreviewKey(messageGUID, rawURL)
			if m.linkPreviewAttempted[key] {
				continue
			}
			if m.linkPreviewInFlight[key] {
				continue
			}
			if m.linkPreviewInFlight == nil {
				m.linkPreviewInFlight = make(map[string]bool)
			}
			m.linkPreviewInFlight[key] = true
			cmds = append(cmds, loadLinkPreviewCmd(m.apiClient, chatGUID, messageGUID, rawURL))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func linkPreviewKey(messageGUID, rawURL string) string {
	return messageGUID + "\x00" + rawURL
}

func loadLinkPreviewCmd(client *api.Client, chatGUID, messageGUID, rawURL string) tea.Cmd {
	return func() tea.Msg {
		preview, err := client.GetLinkPreview(rawURL)
		if err != nil {
			return linkPreviewLoadedMsg{
				chatGUID:    chatGUID,
				messageGUID: messageGUID,
				url:         rawURL,
				preview: models.LinkPreview{
					URL:         rawURL,
					SiteName:    mediaSiteName(rawURL),
					Unavailable: true,
				},
				err: err,
			}
		}
		return linkPreviewLoadedMsg{
			chatGUID:    chatGUID,
			messageGUID: messageGUID,
			url:         rawURL,
			preview: models.LinkPreview{
				URL:         rawURL,
				Title:       preview.Title,
				AuthorName:  preview.AuthorName,
				Description: preview.Description,
				SiteName:    preview.SiteName,
				ImageURL:    preview.ImageURL,
			},
		}
	}
}
