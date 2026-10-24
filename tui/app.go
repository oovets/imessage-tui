package tui

import (
	"errors"
	"fmt"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
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
	chatActionRenameTab
)

// Message types for Bubble Tea
type (
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
	sendReactionSuccessMsg struct{}
	sendReactionErrMsg     struct{ err error }
	providerEventMsg       provider.Event
	wsConnectSuccessMsg    struct{}
	wsConnectFailMsg       struct{ err error }
	wsReconnectedMsg       struct{}
	wsDisconnectedMsg      struct{}
	wsOverflowMsg          struct{}
	refreshTickMsg         struct{}
	markReadSuccessMsg     struct{ chatGUID string }
	markReadErrMsg         struct {
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

	// Backends. Which one owns a chat is derived from its GUID, never stored
	// on the chat, so the two cannot drift — see package provider.
	providers *provider.Registry
	stream    provider.Stream

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
	showChatListTime      bool
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
	// readMarkAgain holds a mark asked for while one was in flight, so the
	// newer messages are still marked once the first answers.
	readMarkAgain map[string]bool
	// readMarkedUpTo is the newest message GUID a chat was marked read at,
	// so the pane's periodic reload marks again only when it brought more.
	readMarkedUpTo       map[string]string
	linkPreviewInFlight  map[string]bool
	linkPreviewAttempted map[string]bool
	// Per backend, not global: BlueBubbles without the private API answers 404
	// to every mark-read, and one shared flag would have silenced Slack's
	// read receipts too — which do work.
	disableReadSync  map[string]bool
	savedLayoutState *config.LayoutState
	didRestoreLayout bool
	chatOverrides    config.ChatOverridesState

	// When the terminal's ESC arrived in a read of its own, leaving the rest
	// of the sequence to be parsed as text. See swallowEscapeSequenceTail.
	lastEscapeAt time.Time

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

	// The last click on a chat in the list, to tell a double click by.
	listClickGUID string
	listClickAt   time.Time

	// unloadedBackends are the backends, by ID, whose chat list has not
	// loaded yet: a timeout at startup, asked again on a timer. See
	// chatload.go.
	unloadedBackends map[string]bool

	// Announcing messages beyond the screen: see notify.go.
	notifyMode    notifyMode
	terminalFocus terminalFocus
	alert         alerter
	alertedAt     map[string]time.Time

	// All disk writes go through this so the Update loop never blocks on
	// fsync/marshal. Blocking Update for even 50ms causes dropped
	// keystrokes during fast typing.
	persist *persister
}

func NewAppModel(backend provider.Provider, stream provider.Stream) AppModel {
	return NewAppModelWithConfig(provider.NewRegistry(backend), stream, nil)
}

// NewAppModelWithConfig builds the app around a registry of backends and one
// realtime stream — merge several with provider.Merge when more than one
// backend is connected. A nil registry is a valid unconfigured state.
func NewAppModelWithConfig(backends *provider.Registry, stream provider.Stream, cfg *config.Config) AppModel {
	ui := config.LoadUIState()
	chatOverrides := config.LoadChatOverrides()
	messageLimit := defaultMessageFetchLimit
	chatLimit := defaultChatFetchLimit
	pollInterval := defaultPollInterval
	enableLinkPreviews := true
	maxPreviewsPerMessage := 2
	notifications := notifyOff
	if cfg != nil {
		notifications = parseNotifyMode(cfg.Notifications)
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
	}

	wm := NewWindowManager()
	wm.SetShowTimestamps(ui.ShowTimestamps)
	wm.SetShowLineNumbers(ui.ShowLineNumbers)
	wm.SetShowSenderNames(ui.ShowSenderNames)
	wm.SetSenderNameChoices(chatOverrides.SenderNames)
	wm.SetShowPaneDividers(ui.ShowPaneDividers)
	app := AppModel{
		chatList:              NewChatListModel(),
		windowManager:         wm,
		providers:             backends,
		stream:                stream,
		focused:               focusChatList,
		width:                 80,
		height:                24,
		showTimestamps:        ui.ShowTimestamps,
		showLineNumbers:       ui.ShowLineNumbers,
		showChatList:          ui.ShowChatList,
		showSenderNames:       ui.ShowSenderNames,
		showPaneDividers:      ui.ShowPaneDividers,
		showChatPreview:       ui.ShowChatPreview,
		showChatListTime:      ui.ShowChatListTime,
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
		readMarkAgain:         make(map[string]bool),
		readMarkedUpTo:        make(map[string]string),
		disableReadSync:       make(map[string]bool),
		linkPreviewInFlight:   make(map[string]bool),
		linkPreviewAttempted:  make(map[string]bool),
		notifyMode:            notifications,
		alert:                 systemAlert,
		alertedAt:             make(map[string]time.Time),
		unloadedBackends:      make(map[string]bool),
		chatOverrides:         chatOverrides,
		persist:               newPersister(),
		loading:               true,
	}
	if layoutState, ok := config.LoadLayoutState(); ok {
		app.savedLayoutState = &layoutState
	}
	app.chatList.SetShowPreview(app.showChatPreview)
	app.chatList.SetShowTime(app.showChatListTime)
	app.chatList.SetNetworks(backends.Label, backends.Labels())
	app.chatList.SetNetwork(ui.ChatListNetwork)
	app.chatList.SetGroupByNetwork(ui.ChatListSort == config.ChatListSortNetwork)
	app.chatList.SetPinned(chatOverrides.Pinned)
	app.chatList.SetIgnored(chatOverrides.Ignored)
	app.restoreMessageCache()
	return app
}

func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		loadChatsCmd(m.providers.All(), m.chatLimit, 0),
		noticeExpireCmd(),
	}

	// Try to connect the realtime stream for live updates
	if m.stream != nil {
		cmds = append(cmds, connectStreamCmd(m.stream))
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
		if msg.attempt > 0 {
			return m, m.addRetriedChats(msg)
		}
		// Before the layout comes back: panes of a network that failed keep
		// their chats (standInChat) instead of coming back empty.
		retry := m.retryFailedChats(msg)
		m.loading = false
		chats := msg.chats
		m.chatList.SetLoading(len(chats) == 0 && retry != nil)
		applyChatOverrides(chats, m.chatOverrides.Aliases)
		m.chatList.SetChats(chats)
		if m.restoreLayoutFromState(chats) {
			m.updateLayout()
			return m, retry
		}
		m.updateLayout()
		cmds := []tea.Cmd{retry}
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
		}
		return m, tea.Batch(cmds...)

	case messagesLoadedMsg:
		// Merge API messages with any WS messages that arrived after the API snapshot.
		// This prevents a race where WS-appended messages disappear when the API
		// response (which may not yet include them) replaces the message list.
		merged := mergeLoadedMessagesWithCache(msg.messages, m.windowManager.GetCachedMessages(msg.chatGUID))
		m.windowManager.SetCachedMessages(msg.chatGUID, merged)
		m.messageFetchInFlight[msg.chatGUID] = false
		m.messageFetchedAt[msg.chatGUID] = time.Now()
		m.saveMessageCacheChat(msg.chatGUID)
		for _, window := range m.windowManager.WindowsShowingChat(msg.chatGUID) {
			window.Messages.SetMessages(merged)
			window.Messages.SetLoading(false)
		}
		// Opening a chat marks it read before its history is in, and Slack
		// marks by the newest timestamp it has seen: the first open had
		// nothing to mark with. Marking again here is what reaches Slack.
		return m, tea.Batch(m.linkPreviewCmdsForMessages(msg.chatGUID, merged), m.markFocusedChatRead(msg.chatGUID))

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

	case sendReactionErrMsg:
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

	case tea.FocusMsg:
		m.terminalFocus = terminalFocused
		return m, nil

	case tea.BlurMsg:
		m.terminalFocus = terminalBlurred
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
		return m, prefetchMessagesCmd(m.providerFor(msg.chatGUID), msg.chatGUID, m.messageLimit)

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
			m.waitForStreamEvent(),
			waitForStreamReconnectCmd(m.stream),
			waitForStreamDisconnectCmd(m.stream),
			waitForStreamOverflowCmd(m.stream),
		)

	case wsDisconnectedMsg:
		m.wsConnected = false
		return m, waitForStreamDisconnectCmd(m.stream)

	case wsOverflowMsg:
		// Events buffer overflowed - at least one event was dropped.
		// Resync every known chat so the missed message surfaces.
		cmds := []tea.Cmd{waitForStreamOverflowCmd(m.stream)}
		if cmd := m.resyncAllChatsCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case wsReconnectedMsg:
		// Socket came back after a drop; any events that arrived while we
		// were offline are lost. Refetch all chats we know about so the
		// user never misses incoming messages silently.
		m.wsConnected = true
		cmds := []tea.Cmd{waitForStreamReconnectCmd(m.stream)}
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
		if m.readMarkAgain[msg.chatGUID] {
			delete(m.readMarkAgain, msg.chatGUID)
			return m, m.markChatReadIfNeeded(msg.chatGUID)
		}
		return m, nil

	case markReadErrMsg:
		m.readMarkInFlight[msg.chatGUID] = false
		delete(m.readMarkAgain, msg.chatGUID)
		// Marked nothing, so the next load or message tries again.
		delete(m.readMarkedUpTo, msg.chatGUID)
		low := strings.ToLower(msg.err.Error())
		// A backend that cannot mark chats read says so the same way every
		// time: BlueBubbles without the private API helper, Slack with a token
		// that lacks the write scope. Retrying on every chat you open just
		// fills the log, so the backend is taken at its word.
		if strings.Contains(low, "status 404") || strings.Contains(low, "private api") ||
			strings.Contains(low, "status 501") || strings.Contains(low, "missing_scope") {
			backend := m.backendIDFor(msg.chatGUID)
			m.disableReadSync[backend] = true
			log.Printf("disabling remote read sync for %s: %v", backend, msg.err)
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

	case providerEventMsg:
		return m.handleProviderEvent(provider.Event(msg))

	case errMsg:
		m.setAppError(msg.err)
		return m, nil

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

	case tea.KeyMsg:
		if m.swallowEscapeSequenceTail(msg) {
			return m, nil
		}
		if m.showHelp {
			switch msg.String() {
			case "?", "f1", "esc", "q", "ctrl+c":
				m.showHelp = false
			}
			return m, nil
		}
		if m.chatAction != chatActionNone {
			return m.handleChatActionKey(msg)
		}

		// Emoji autocomplete grabs navigation/accept/dismiss keys before they
		// reach the normal input and app handlers (so Enter accepts a suggestion
		// instead of sending the message).
		if m.focused == focusWindow {
			if window := m.windowManager.FocusedWindow(); window != nil && window.Input.AutocompleteActive() {
				if handled, cmd := window.Input.HandleAutocompleteKey(msg); handled {
					return m, cmd
				}
			}
		}

		m.lastKey = msg.String()

		// While a text field has focus, editing keys belong to the text being
		// written — not to the global command table. Without this the table
		// below swallowed them wherever they appeared: "?" opened help, "q"
		// quit the app mid-message, and the arrow keys jumped between panes
		// instead of moving the cursor.
		if m.typingFocus() && keyEditsText(msg) {
			return m.delegateToFocused(msg)
		}

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
			case "n", "N":
				step := 1
				if msg.String() == "N" {
					step = -1
				}
				network, ok := m.chatList.CycleNetwork(step)
				if !ok {
					return m, showToastCmd("Only one network in the list", 2*time.Second)
				}
				m.saveUIState()
				if network == "" {
					network = "all"
				}
				return m, showToastCmd("Network: "+network, 2*time.Second)
			case "p":
				if chat := m.chatList.SelectedChat(); chat != nil {
					return m, m.togglePin(chat)
				}
			case "x":
				if chat := m.chatList.SelectedChat(); chat != nil {
					name := chat.GetDisplayName()
					toast := "No longer ignoring " + name
					if m.chatList.ToggleIgnore(chat.GUID) {
						toast = "Ignored " + name + " — I shows ignored chats"
					}
					m.savePins()
					return m, showToastCmd(toast, 3*time.Second)
				}
			case "I":
				show := !m.chatList.ShowIgnored()
				m.chatList.SetShowIgnored(show)
				toast := "Ignored chats hidden"
				if show {
					toast = "Showing ignored chats — x on one stops ignoring it"
					if len(m.chatList.Ignored()) == 0 {
						toast = "No ignored chats"
					}
				}
				return m, showToastCmd(toast, 3*time.Second)
			case "t":
				m.showChatListTime = !m.showChatListTime
				m.chatList.SetShowTime(m.showChatListTime)
				m.saveUIState()
				return m, nil
			case "U":
				show := !m.chatList.ShowUnknown()
				m.chatList.SetShowUnknown(show)
				toast := "Unknown senders hidden"
				if show {
					toast = "Showing " + itoa(m.chatList.UnknownCount()) + " chats from unknown senders"
				}
				return m, showToastCmd(toast, 3*time.Second)
			case "K", "J":
				if chat := m.chatList.SelectedChat(); chat != nil {
					if !m.chatList.IsPinned(chat.GUID) {
						return m, showToastCmd("Only pinned chats can be moved — p pins", 2*time.Second)
					}
					step := 1
					if msg.String() == "K" {
						step = -1
					}
					if m.chatList.MovePin(chat.GUID, step) {
						m.savePins()
					}
					return m, nil
				}
			case "s":
				group := !m.chatList.GroupByNetwork()
				m.chatList.SetGroupByNetwork(group)
				m.saveUIState()
				toast := "Sort: recent"
				if group {
					toast = "Sort: by network"
					if m.chatList.NetworkCount() < 2 {
						toast += " (only one network for now)"
					}
				}
				return m, showToastCmd(toast, 2*time.Second)
			}
		}
		if m.focused == focusWindow {
			switch msg.String() {
			case "ctrl+o":
				if window := m.windowManager.FocusedWindow(); window != nil && window.Chat != nil {
					return m, m.togglePin(window.Chat)
				}
				return m, nil
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
		case "?", "f1":
			// The help overlay and the empty-pane hint both advertise F1, so
			// it has to actually open the thing.
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
			if err := m.windowManager.SplitWindow(SplitHorizontal); err != nil {
				return m, showToastCmd(capitalize(err.Error()), 3*time.Second)
			}
			m.updateLayout()
			m.saveLayoutState()
			return m, nil

		case "ctrl+g":
			if err := m.windowManager.SplitWindow(SplitVertical); err != nil {
				return m, showToastCmd(capitalize(err.Error()), 3*time.Second)
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

		// Tabs. Alt, not ctrl: the ctrl keys are taken, and alt-modified
		// keys never count as text, so these work mid-message too. alt+[ is
		// not one of them — it is how a torn escape sequence arrives, and
		// swallowEscapeSequenceTail drops it.
		case "alt+t":
			if err := m.windowManager.NewTab(); err != nil {
				return m, showToastCmd(capitalize(err.Error()), 3*time.Second)
			}
			return m, m.afterTabChange()

		case "alt+w":
			if !m.windowManager.CloseTab() {
				return m, showToastCmd("Last tab — alt+t opens another", 2*time.Second)
			}
			return m, m.afterTabChange()

		case "alt+.", "alt+,":
			step := 1
			if msg.String() == "alt+," {
				step = -1
			}
			if !m.windowManager.CycleTab(step) {
				return m, nil
			}
			return m, m.afterTabChange()

		case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
			if !m.windowManager.SwitchTab(int(msg.Runes[0] - '1')) {
				return m, nil
			}
			return m, m.afterTabChange()

		case "alt+n":
			return m, m.jumpToUnread()

		case "alt+r":
			m.chatAction = chatActionRenameTab
			m.renameText = m.windowManager.ActiveTabName()
			return m, nil

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

		case "ctrl+b":
			// Sender names for the chat in the focused pane. Chats differ —
			// a Slack channel needs names, a two-person thread does not — so
			// the toggle answers for the one you are looking at, and the
			// answer is kept with the chat's other overrides.
			show := m.windowManager.ToggleFocusedSenderNames()
			m.saveSenderNameChoices()
			return m, showToastCmd(senderNameToast(show, "this chat"), 2*time.Second)

		case "alt+m", "ctrl+m":
			// The same toggle for every pane, and the default new panes start
			// from. ctrl+m is kept for terminals that can distinguish it from
			// Enter.
			m.showSenderNames = !m.showSenderNames
			m.windowManager.SetShowSenderNames(m.showSenderNames)
			m.saveUIState()
			toast := "Sender names in every chat"
			if !m.showSenderNames {
				toast = "Sender names in group chats only"
			}
			return m, showToastCmd(toast, 2*time.Second)

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

		case "esc", "escape":
			// Bubble Tea names this key "esc"; matching only "escape" made this
			// branch unreachable, so the documented "esc returns to the chat
			// list" silently did nothing.
			// Always go to chat list from a window
			if m.focused == focusWindow && m.showChatList {
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Blur()
				}
				m.focused = focusChatList
			}
			return m, nil

		// Pane navigation. Plain arrows only reach here when no text field has
		// focus; shift+←/→ always does, so horizontally split panes stay
		// reachable mid-message (ctrl+↑/↓ covers the vertical axis).
		case "left", "shift+left":
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

		case "right", "shift+right":
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
				return m, m.openSelectedChat()
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
							sendMessageCmd(m.providerFor(window.Chat.GUID), window.Chat.GUID, text, "", window.ID, pending.GUID),
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

	return m.delegateToFocused(msg)
}

// keyEditsText reports whether a key is one a focused text field owns: plain
// characters and unmodified cursor movement.
//
// Deliberately excluded so they keep their global meaning while composing:
// enter (send), tab/esc (focus), pgup/pgdown/end (scroll history), and every
// ctrl-/alt-modified key, which is what keeps the full command set reachable
// mid-message. Pane navigation moves to ctrl+↑/↓ and shift+←/→.
func keyEditsText(msg tea.KeyMsg) bool {
	if msg.Alt {
		return false
	}
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace,
		tea.KeyLeft, tea.KeyRight, tea.KeyUp, tea.KeyDown:
		return true
	}
	return false
}

// typingFocus reports whether keystrokes are currently landing in a text field
// — a chat's message composer or the chat list's search box — and therefore
// must not be read as global commands.
func (m AppModel) typingFocus() bool {
	switch m.focused {
	case focusChatList:
		return m.chatList.SearchActive()
	case focusWindow:
		window := m.windowManager.FocusedWindow()
		return window != nil && window.Chat != nil && window.Input.Focused()
	}
	return false
}

// delegateToFocused hands a message straight to the focused component.
func (m AppModel) delegateToFocused(msg tea.Msg) (AppModel, tea.Cmd) {
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
		ShowChatListTime: m.showChatListTime,
		ChatListWidth:    m.chatListWidth,
		ChatListNetwork:  m.chatList.Network(),
		ChatListSort:     chatListSort(m.chatList.GroupByNetwork()),
	})
}

func chatListSort(groupByNetwork bool) string {
	if groupByNetwork {
		return config.ChatListSortNetwork
	}
	return config.ChatListSortRecent
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
	delete(m.readMarkAgain, chatGUID)
	delete(m.readMarkedUpTo, chatGUID)
	delete(m.chatOverrides.Aliases, chatGUID)
	m.windowManager.ForgetSenderNameChoice(chatGUID)
	m.chatOverrides.SenderNames = m.windowManager.SenderNameChoices()
	if m.chatList.IsPinned(chatGUID) {
		m.chatList.TogglePin(chatGUID)
	}
	if m.chatList.IsIgnored(chatGUID) {
		m.chatList.ToggleIgnore(chatGUID)
	}
	m.savePins()
}

// togglePin pins or unpins a chat, saves the pins and says which it did.
func (m *AppModel) togglePin(chat *models.Chat) tea.Cmd {
	name := chat.GetDisplayName()
	toast := "Unpinned " + name
	if m.chatList.TogglePin(chat.GUID) {
		toast = "Pinned " + name
	}
	m.savePins()
	return showToastCmd(toast, 2*time.Second)
}

// savePins writes the chat list's pins and ignores. Both live beside the
// aliases in chat_overrides.json, and ignoring a chat can unpin it.
func (m *AppModel) savePins() {
	m.chatOverrides.Pinned = m.chatList.Pinned()
	m.chatOverrides.Ignored = m.chatList.Ignored()
	_ = config.SaveChatOverrides(m.chatOverrides)
}

// saveSenderNameChoices writes the per-chat sender-name choices beside the
// aliases and pins.
func (m *AppModel) saveSenderNameChoices() {
	m.chatOverrides.SenderNames = m.windowManager.SenderNameChoices()
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
			return m, deleteChatCmd(m.providerFor(chatGUID), chatGUID)
		default:
			return m, nil
		}
	case chatActionRename, chatActionRenameTab:
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
			if m.chatAction == chatActionRenameTab {
				m.windowManager.SetActiveTabName(m.renameText)
				m.clearChatAction()
				m.saveLayoutState()
				return m, nil
			}
			chatGUID := m.actionChatGUID
			displayName := strings.TrimSpace(m.renameText)
			m.clearChatAction()
			return m, renameChatCmd(m.providerFor(chatGUID), chatGUID, displayName)
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
	contentHeight := m.height
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Calculate chat list dimensions (no borders, just padding)
	chatListContentHeight := contentHeight
	chatListWidth := 0
	if m.showChatList {
		chatListWidth = m.chatListWidth
	}
	chatListInnerWidth := max(1, chatListWidth-3)
	m.chatList.SetSize(chatListInnerWidth, chatListContentHeight)
	m.chatList.SetFocused(m.focused == focusChatList && m.showChatList)

	// Calculate window area (everything to the right of chat list)
	windowsWidth := m.width - 2 // -2 for padding
	if m.showChatList {
		windowsWidth -= m.chatListWidth
	}
	if windowsWidth < 1 {
		windowsWidth = 1
	}
	windowsHeight := contentHeight - m.tabBarRows()
	if windowsHeight < 1 {
		windowsHeight = 1
	}

	m.windowManager.SetSize(windowsWidth, windowsHeight)
}

// tabBarRows is how many rows the tab bar takes above the panes. With a
// single tab there is nothing to choose between, so there is no bar and the
// panes keep the row.
func (m AppModel) tabBarRows() int {
	if m.windowManager.TabCount() > 1 && m.height > 1 {
		return 1
	}
	return 0
}

// panesX is the screen column where the pane area starts.
func (m AppModel) panesX() int {
	if m.showChatList {
		return m.chatListWidth
	}
	return 0
}

// tabTitles returns what the bar calls each tab.
func (m AppModel) tabTitles(infos []TabInfo) []string {
	titles := make([]string, len(infos))
	for i, info := range infos {
		titles[i] = tabTitle(info, m.providers.Label)
	}
	return titles
}

// tabBarView draws the tab bar over the pane area, or "" with one tab.
func (m AppModel) tabBarView() string {
	if m.tabBarRows() == 0 {
		return ""
	}
	infos := m.windowManager.Tabs()
	return renderTabBar(infos, m.tabTitles(infos), m.tabBarWidth())
}

// afterTabChange settles the app on whichever tab is now on screen: the bar
// may have appeared or gone, the composer focus follows the pane focus, and
// the new tab's chats are fetched if they went stale while it was hidden —
// the refresh timer only reloads the tab on screen.
func (m *AppModel) afterTabChange() tea.Cmd {
	m.dividerDragging = false
	m.dividerNode = nil
	m.updateLayout()
	if m.focused == focusWindow {
		if window := m.windowManager.FocusedWindow(); window != nil {
			window.Input.textarea.Focus()
		}
	}
	m.saveLayoutState()
	m.followTabNetwork()

	cmds := []tea.Cmd{m.clearFocusedWindowNewMessageIndicator()}
	seen := make(map[string]bool)
	for _, window := range m.windowManager.AllWindows() {
		if window.Chat == nil || seen[window.Chat.GUID] {
			continue
		}
		chatGUID := window.Chat.GUID
		seen[chatGUID] = true
		if !m.shouldRefreshMessages(chatGUID) {
			continue
		}
		m.messageFetchInFlight[chatGUID] = true
		cmds = append(cmds, prefetchMessagesCmd(m.providerFor(chatGUID), chatGUID, m.messageLimit))
	}
	return tea.Batch(cmds...)
}

// followTabNetwork points the chat list's network filter at the tab on
// screen, so a tab of Slack channels lists Slack: the tab's one network, or
// every network for a tab that mixes them — the same thing its title says.
// An empty tab leaves the filter as it was; the list is where its first chat
// is picked, from whichever network the last tab was on.
func (m *AppModel) followTabNetwork() {
	infos := m.windowManager.Tabs()
	networks := tabNetworks(infos[m.windowManager.ActiveTab()], m.providers.Label)
	network := ""
	switch len(networks) {
	case 0:
		return
	case 1:
		network = networks[0]
	}
	if network == m.chatList.Network() {
		return
	}
	m.chatList.SetNetwork(network)
	m.saveUIState()
}

// tabBarWidth is how much of the top row the tab bar may use: all of the pane
// area's, less the corner the unread badge is drawn over.
func (m AppModel) tabBarWidth() int {
	return max(m.windowManager.width-lipgloss.Width(m.unreadBadge()), 1)
}

// tabAt returns the tab drawn at column x of the tab bar.
func (m AppModel) tabAt(x int) (int, bool) {
	infos := m.windowManager.Tabs()
	_, spans := tabBarLabels(infos, m.tabTitles(infos), m.tabBarWidth())
	for _, span := range spans {
		if x >= span.x0 && x < span.x1 {
			return span.tab, true
		}
	}
	return 0, false
}

func (m AppModel) View() string {
	// Settled once over the whole frame, after every pane, list and popup has
	// been padded to its width, so no layout code has to know about it.
	return settleEmojiWidths(m.frame())
}

func (m AppModel) frame() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	if m.showHelp {
		return helpOverlayView(m.width, m.height)
	}

	// Render chat list panel over the full frame height: there is no status
	// bar row to reserve any more.
	chatPanel := ""
	if m.showChatList {
		chatListStyle := PanelStyle
		if m.focused == focusChatList {
			chatListStyle = ActivePanelStyle
		}
		panelHeight := m.height
		if panelHeight < 1 {
			panelHeight = 1
		}
		chatPanel = chatListStyle.
			Width(m.chatListWidth).
			MaxWidth(m.chatListWidth).
			Height(panelHeight).
			MaxHeight(panelHeight).
			Render(m.chatList.View())
	}

	// Render windows area
	windowsView := m.windowManager.Render()
	if bar := m.tabBarView(); bar != "" {
		windowsView = lipgloss.JoinVertical(lipgloss.Left, bar, windowsView)
	}

	// Join panels horizontally
	content := windowsView
	if m.showChatList {
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			chatPanel,
			windowsView,
		)
	}

	if badge := m.unreadBadge(); badge != "" {
		content = overlayTopRight(content, badge, m.frameWidth())
	}
	if line := m.statusLineView(); line != "" {
		content = overlayBottomLine(content, line)
	}
	return content
}

// statusLineView renders the transient status line, or "" when there is
// nothing to report. There is no persistent status bar any more, so this only
// carries things the user must see while they are happening: chat action
// prompts, errors and toasts. Persistent chrome (connection dot, pane index,
// help hint) is gone with the bar.
func (m AppModel) statusLineView() string {
	var parts []string

	if actionText := m.chatActionStatusText(); actionText != "" {
		parts = append(parts, actionText)
	}

	now := time.Now()
	if m.err != nil && !m.errUntil.IsZero() && now.Before(m.errUntil) {
		errText := m.err.Error()
		if len(errText) > 48 {
			errText = errText[:47] + "…"
		}
		parts = append(parts, "! "+errText)
	}
	if m.toastText != "" && !m.toastUntil.IsZero() && now.Before(m.toastUntil) {
		parts = append(parts, m.toastText)
	}

	if len(parts) == 0 {
		return ""
	}

	style := StatusLineStyle
	if m.width > 0 {
		style = style.Width(m.width).MaxWidth(m.width)
	}
	return style.Render(strings.Join(parts, "  "))
}

// overlayBottomLine replaces the last row of view with line instead of
// appending one, so the frame stays exactly m.height rows tall and the alt
// screen never scrolls.
func overlayBottomLine(view, line string) string {
	rows := strings.Split(view, "\n")
	if len(rows) == 0 {
		return line
	}
	rows[len(rows)-1] = line
	return strings.Join(rows, "\n")
}

func (m AppModel) chatActionStatusText() string {
	switch m.chatAction {
	case chatActionDelete:
		return fmt.Sprintf("Delete %q? Press D to confirm, Esc to cancel", m.actionChatName)
	case chatActionRename:
		return fmt.Sprintf("Rename %q: %s", m.actionChatName, m.renameText)
	case chatActionRenameTab:
		return fmt.Sprintf("Name tab %d (empty: name it after its networks): %s", m.windowManager.ActiveTab()+1, m.renameText)
	default:
		return ""
	}
}

// Command constructors

// escapeTailWindow is how long after a torn escape sequence its remainder is
// still expected.
//
// Generous on purpose. The split happens when the app is busy, so the two
// halves can be a whole render apart — eight panes of history is well over the
// 60ms this first used. Still far shorter than the gap between a keypress and
// someone typing a plausible sequence by hand.
const escapeTailWindow = 250 * time.Millisecond

// mouseReportTail matches what is left of an SGR mouse report: "<35;15;33M",
// "[<0;4;7m". Terminals emit these constantly while the mouse moves, so they
// are by far the most common thing to arrive torn — and nothing a person types
// looks like one, so this needs no timing guard at all.
var mouseReportTail = regexp.MustCompile(`^\[?<[0-9;]+[Mm]$`)

// escapeSequenceTail matches the remainder of a CSI or SS3 sequence, with or
// without its introducer: "[1;6C", "1;6C", "[27;5;9~", "Oc".
var escapeSequenceTail = regexp.MustCompile(`^[\[O]?[0-9;:<=>?]*[ -/]*[@-~]$`)

// escapeSequencePartialTail matches the same thing cut short again, for a
// sequence that arrived in more than two pieces.
var escapeSequencePartialTail = regexp.MustCompile(`^[\[O]?[0-9;:<=>?]+$`)

// swallowEscapeSequenceTail drops the text left over when a key's escape
// sequence was torn apart before it reached us.
//
// Bubble Tea's input reader assumes a read shorter than its buffer ends on an
// event boundary (bubbletea key.go, readAnsiInputs: `canHaveMoreData :=
// numBytes == len(buf)`). A terminal does not promise that. When the app is
// busy — a pane switch is the most expensive frame it draws — the kernel hands
// over part of the sequence and the rest on the next read, and the parser has
// already committed. "\x1b[1;6C" then arrives as either a lone Escape or an
// alt+[ , followed by ordinary runes: the literal text "[1;6C" typed into
// whichever composer had focus, which is the pane being switched away from.
//
// Only the leftovers are dropped. Requiring a torn start within the last few
// milliseconds is what keeps this from eating text that merely looks like a
// sequence.
func (m *AppModel) swallowEscapeSequenceTail(msg tea.KeyMsg) bool {
	// A mouse report is unmistakable, and by the time one is torn the app has
	// usually been busy long enough for any timing window to have closed.
	if msg.Type == tea.KeyRunes && !msg.Alt && mouseReportTail.MatchString(string(msg.Runes)) {
		m.lastEscapeAt = time.Time{}
		return true
	}
	if isTornSequenceStart(msg) {
		m.lastEscapeAt = time.Now()
		// alt+[ carries the introducer as text and would otherwise be typed;
		// a bare Escape is a real key and still has to reach the handlers.
		return msg.Type == tea.KeyRunes
	}
	if msg.Type != tea.KeyRunes || msg.Alt || m.lastEscapeAt.IsZero() {
		return false
	}
	if time.Since(m.lastEscapeAt) > escapeTailWindow {
		m.lastEscapeAt = time.Time{}
		return false
	}

	runes := string(msg.Runes)
	switch {
	case escapeSequenceTail.MatchString(runes):
		m.lastEscapeAt = time.Time{}
		return true
	case escapeSequencePartialTail.MatchString(runes):
		// Still mid-sequence: keep the window open for the remaining pieces.
		m.lastEscapeAt = time.Now()
		return true
	default:
		m.lastEscapeAt = time.Time{}
		return false
	}
}

// isTornSequenceStart reports whether a key looks like the head of an escape
// sequence that lost the rest of itself: a bare Escape, or the introducer
// delivered as alt+[ / alt+O.
func isTornSequenceStart(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyEscape && !msg.Alt {
		return true
	}
	return msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1 &&
		(msg.Runes[0] == '[' || msg.Runes[0] == 'O')
}

// senderNameToast says what a sender-name toggle just did. Without it the
// per-chat and every-chat bindings look identical from the outside.
func senderNameToast(show bool, scope string) string {
	if show {
		return "Sender names on (" + scope + ")"
	}
	return "Sender names off (" + scope + ")"
}

// capitalize upper-cases the first letter, so an error written as a Go error
// string reads as a sentence in a toast.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// windowChatGUID is the GUID of the chat a pane shows, or "" for an empty
// pane — which routes to the default backend, the same one an unprefixed GUID
// would reach.
func windowChatGUID(window *ChatWindow) string {
	if window == nil || window.Chat == nil {
		return ""
	}
	return window.Chat.GUID
}

// backendIDFor names the backend owning a chat, for state that is per backend
// rather than per chat. Chats with no backend share one bucket, which is where
// they belong: there is nothing to talk to for any of them.
func (m AppModel) backendIDFor(chatGUID string) string {
	if backend := m.providerFor(chatGUID); backend != nil {
		return backend.ID()
	}
	return ""
}

// providerFor returns the backend owning chatGUID, or nil when none is
// configured. Every call that talks to a backend goes through here.
func (m AppModel) providerFor(chatGUID string) provider.Provider {
	return m.providers.For(chatGUID)
}

// loadChatsCmd fans out over every configured backend and concatenates the
// result. One backend being down must not blank the list, so it is only an
// error when nothing loaded at all.
// chatsLoadedMsg is the chat lists of every backend asked. A backend that
// failed is named rather than failing the rest, so it can be asked again.
type chatsLoadedMsg struct {
	chats   []models.Chat
	loaded  []provider.Provider
	failed  []provider.Provider
	attempt int // 0 for the load at startup, then each retry's number
}

func loadChatsCmd(backends []provider.Provider, limit, attempt int) tea.Cmd {
	return func() tea.Msg {
		msg := chatsLoadedMsg{attempt: attempt}
		for _, backend := range backends {
			chats, err := backend.Chats(limit)
			if err != nil {
				log.Printf("chat load failed: %s: %v", backend.ID(), err)
				msg.failed = append(msg.failed, backend)
				continue
			}
			msg.loaded = append(msg.loaded, backend)
			msg.chats = append(msg.chats, chats...)
		}
		return msg
	}
}

func loadMessagesCmd(backend provider.Provider, chatGUID string, limit int) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("no backend for chat %s", chatGUID), report: true}
		}
		messages, err := backend.Messages(chatGUID, limit)
		if err != nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("failed to load messages: %v", err), report: true}
		}
		return messagesLoadedMsg{chatGUID: chatGUID, messages: messages}
	}
}

func prefetchMessagesCmd(backend provider.Provider, chatGUID string, limit int) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("no backend for chat %s", chatGUID), report: false}
		}
		messages, err := backend.Messages(chatGUID, limit)
		if err != nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("failed to prefetch messages: %v", err), report: false}
		}
		return messagesLoadedMsg{chatGUID: chatGUID, messages: messages}
	}
}

// openSelectedChat opens the chat under the list's cursor in the focused pane
// and moves to its composer: Enter in the list, or a double click on a chat.
func (m *AppModel) openSelectedChat() tea.Cmd {
	selected := m.chatList.SelectedChat()
	window := m.windowManager.FocusedWindow()
	if selected == nil || window == nil {
		return nil
	}
	m.focused = focusWindow
	m.chatList.SetFocused(false)
	window.Input.textarea.Focus()
	return m.selectChat(selected, window)
}

// doubleClickWindow is how close together two clicks on one chat in the list
// must be to open it.
const doubleClickWindow = 500 * time.Millisecond

// listDoubleClick notes a click on a chat in the list ("" for a click on
// anything else) and reports whether it completes a double click: the second
// of two on the same chat in quick succession. A third click starts over
// rather than counting as another double.
func (m *AppModel) listDoubleClick(chatGUID string) bool {
	now := time.Now()
	double := chatGUID != "" && chatGUID == m.listClickGUID && now.Sub(m.listClickAt) <= doubleClickWindow
	if double {
		m.listClickGUID, m.listClickAt = "", time.Time{}
		return true
	}
	m.listClickGUID, m.listClickAt = chatGUID, now
	return false
}

func (m *AppModel) selectChat(chat *models.Chat, window *ChatWindow) tea.Cmd {
	if chat == nil || window == nil {
		return nil
	}

	m.windowManager.ShowChat(window, chat)
	m.chatList.ClearNewMessage(chat.GUID)
	m.saveLayoutState()

	if cached := m.windowManager.GetCachedMessages(chat.GUID); len(cached) > 0 {
		window.Messages.SetMessages(cached)
	}

	var cmds []tea.Cmd
	if m.shouldRefreshMessages(chat.GUID) {
		m.messageFetchInFlight[chat.GUID] = true
		window.Messages.SetLoading(true)
		cmds = append(cmds, loadMessagesCmd(m.providerFor(chat.GUID), chat.GUID, m.messageLimit))
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

// saveMessageCacheChat persists just one chat's cached messages. Use this on
// hot single-chat paths (incoming WS messages, per-chat reloads) so we don't
// deep-copy every cached chat on every message; the persister merges it into
// the cumulative state. Broad changes (deletes, shutdown) still use
// saveMessageCache to rebuild the whole state.
func (m *AppModel) saveMessageCacheChat(chatGUID string) {
	chatGUID = strings.TrimSpace(chatGUID)
	if chatGUID == "" {
		return
	}
	messages := m.windowManager.GetCachedMessages(chatGUID)
	if len(messages) == 0 {
		return
	}
	// GetCachedMessages returns the live slice; copy it so the debounced flush
	// can't race a later append on the Update loop.
	copied := make([]models.Message, len(messages))
	copy(copied, messages)

	fetchedAt := time.Now().UnixMilli()
	if t, ok := m.messageFetchedAt[chatGUID]; ok && !t.IsZero() {
		fetchedAt = t.UnixMilli()
	}
	m.persist.saveMessagesChat(chatGUID, config.CachedChatMessages{
		Messages:           copied,
		FetchedAtUnixMilli: fetchedAt,
	})
}

func (m *AppModel) restoreLayoutFromState(chats []models.Chat) bool {
	if m.didRestoreLayout {
		return false
	}
	m.didRestoreLayout = true

	if m.savedLayoutState == nil {
		return false
	}
	states, active := m.savedLayoutState.TabStates()
	if len(states) == 0 {
		return false
	}

	chatByGUID := make(map[string]*models.Chat, len(chats))
	for i := range chats {
		chatByGUID[chats[i].GUID] = &chats[i]
	}

	// A tab that no longer parses is dropped rather than failing the rest.
	var restored []restoredTab
	restoredActive := -1
	for i, state := range states {
		tab, ok := m.buildTab(state, chatByGUID)
		if !ok {
			continue
		}
		if i == active {
			restoredActive = len(restored)
		}
		restored = append(restored, tab)
	}
	if len(restored) == 0 {
		return false
	}
	if restoredActive < 0 {
		restoredActive = 0
	}

	m.windowManager.setTabs(restored, restoredActive)
	m.focused = focusWindow
	m.clearFocusedWindowNewMessageIndicator()
	return true
}

// buildTab rebuilds one saved tab: its split tree, with each leaf showing the
// chat it showed before, from the cache until the next fetch.
func (m *AppModel) buildTab(state config.TabLayoutState, chatByGUID map[string]*models.Chat) (restoredTab, bool) {
	wm := m.windowManager
	windows := make(map[WindowID]*ChatWindow)
	var leaves []*ChatWindow
	var nextID WindowID

	var build func(node *config.LayoutNodeState) *LayoutNode
	build = func(node *config.LayoutNodeState) *LayoutNode {
		if node == nil {
			return nil
		}

		dir := SplitDirection(node.Direction)
		if dir == SplitNone {
			window := wm.newWindow(nextID)
			windows[nextID] = window
			nextID++
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

	root := build(state.Root)
	if root == nil || len(leaves) == 0 || len(leaves) > wm.maxWindows {
		return restoredTab{}, false
	}

	for i, window := range leaves {
		if i >= len(state.LeafChatGUIDs) {
			continue
		}
		chatGUID := strings.TrimSpace(state.LeafChatGUIDs[i])
		chat, ok := chatByGUID[chatGUID]
		if !ok {
			chat, ok = m.threadChat(chatGUID)
		}
		if !ok && chatGUID != "" && m.unloadedBackends[m.backendIDFor(chatGUID)] {
			chat, ok = m.standInChat(chatGUID), true
		}
		if !ok {
			continue
		}
		wm.ShowChat(window, chat)
		if cached := wm.GetCachedMessages(chatGUID); len(cached) > 0 {
			window.Messages.SetMessages(cached)
		}
	}

	focusIdx := state.FocusedLeafIndex
	if focusIdx < 0 || focusIdx >= len(leaves) {
		focusIdx = 0
	}
	return restoredTab{
		name:    state.Name,
		root:    root,
		windows: windows,
		focused: leaves[focusIdx].ID,
	}, true
}

func (m *AppModel) saveLayoutState() {
	if state, ok := m.layoutState(); ok {
		m.persist.saveLayout(state)
	}
}

// layoutState is every tab's layout in its saved form.
func (m *AppModel) layoutState() (config.LayoutState, bool) {
	wm := m.windowManager
	if wm == nil || wm.root == nil {
		return config.LayoutState{}, false
	}

	var tabs []config.TabLayoutState
	active := 0
	for i, tab := range wm.tabs {
		root, windows, focused := tab.root, tab.windows, tab.focusedWindow
		if i == wm.activeTab {
			root, windows, focused = wm.root, wm.windows, wm.focusedWindow
		}
		state, ok := encodeTabLayout(root, windows, focused)
		if !ok {
			continue
		}
		if i == wm.activeTab {
			active = len(tabs)
		}
		state.Name = tab.name
		tabs = append(tabs, state)
	}
	if len(tabs) == 0 {
		return config.LayoutState{}, false
	}

	current := tabs[active]
	return config.LayoutState{
		Root:             current.Root,
		LeafChatGUIDs:    current.LeafChatGUIDs,
		FocusedLeafIndex: current.FocusedLeafIndex,
		Tabs:             tabs,
		ActiveTab:        active,
	}, true
}

// encodeTabLayout turns one tab's split tree into its saved form.
func encodeTabLayout(rootNode *LayoutNode, windows map[WindowID]*ChatWindow, focused WindowID) (config.TabLayoutState, bool) {
	if rootNode == nil {
		return config.TabLayoutState{}, false
	}
	leafOrder := make([]WindowID, 0, rootNode.CountWindows())
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

	root := encode(rootNode)
	if root == nil || len(leafOrder) == 0 {
		return config.TabLayoutState{}, false
	}

	leafChatGUIDs := make([]string, len(leafOrder))
	focusedLeafIndex := 0
	for i, id := range leafOrder {
		window := windows[id]
		if window != nil && window.Chat != nil {
			leafChatGUIDs[i] = window.Chat.GUID
		}
		if id == focused {
			focusedLeafIndex = i
		}
	}

	return config.TabLayoutState{
		Root:             root,
		LeafChatGUIDs:    leafChatGUIDs,
		FocusedLeafIndex: focusedLeafIndex,
	}, true
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
	if chatGUID == "" || m.disableReadSync[m.backendIDFor(chatGUID)] {
		return nil
	}
	if m.readMarkInFlight[chatGUID] {
		m.readMarkAgain[chatGUID] = true
		return nil
	}
	m.readMarkInFlight[chatGUID] = true
	m.readMarkedUpTo[chatGUID] = newestMessageGUID(m.windowManager.GetCachedMessages(chatGUID))
	return markChatReadCmd(m.providerFor(chatGUID), chatGUID)
}

// markFocusedChatRead marks a chat read on the server when it is the one being
// read — in the focused pane, with the focus on the panes — and it holds a
// message newer than the last mark. Without this, Slack only heard about a
// chat when it was opened, so whatever arrived while it was open stayed
// unread in Slack's own apps.
func (m *AppModel) markFocusedChatRead(chatGUID string) tea.Cmd {
	if m.focused != focusWindow {
		return nil
	}
	window := m.windowManager.FocusedWindow()
	if window == nil || window.Chat == nil || window.Chat.GUID != chatGUID {
		return nil
	}
	newest := newestMessageGUID(m.windowManager.GetCachedMessages(chatGUID))
	if newest == "" || newest == m.readMarkedUpTo[chatGUID] {
		return nil
	}
	return m.markChatReadIfNeeded(chatGUID)
}

func newestMessageGUID(msgs []models.Message) string {
	var guid string
	var at int64
	for i := range msgs {
		if msgs[i].GUID != "" && msgs[i].DateCreated >= at {
			guid, at = msgs[i].GUID, msgs[i].DateCreated
		}
	}
	return guid
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
		cmds = append(cmds, prefetchMessagesCmd(m.providerFor(chatGUID), chatGUID, m.messageLimit))
	}

	if len(cmds) == 0 {
		return nil
	}

	return tea.Batch(cmds...)
}

// sendMessageCmd delivers text, optionally as a reply to replyToGUID — a
// threaded reply on Slack, a quoted reply on iMessage.
func sendMessageCmd(backend provider.Provider, chatGUID, text, replyToGUID string, windowID WindowID, pendingGUID string) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return sendErrMsg{err: errNoBackend(chatGUID), chatGUID: chatGUID, pendingGUID: pendingGUID}
		}
		if err := backend.Send(chatGUID, text, replyToGUID, pendingGUID); err != nil {
			return sendErrMsg{err: err, chatGUID: chatGUID, pendingGUID: pendingGUID}
		}
		return sendSuccessMsg{windowID: windowID}
	}
}

func sendReactionCmd(backend provider.Provider, chatGUID, messageGUID, reaction string) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return sendReactionErrMsg{err: errNoBackend(chatGUID)}
		}
		if err := backend.React(chatGUID, messageGUID, reaction); err != nil {
			return sendReactionErrMsg{err: err}
		}
		return sendReactionSuccessMsg{}
	}
}

func markChatReadCmd(backend provider.Provider, chatGUID string) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return markReadErrMsg{chatGUID: chatGUID, err: errNoBackend(chatGUID)}
		}
		if err := backend.MarkRead(chatGUID); err != nil {
			return markReadErrMsg{chatGUID: chatGUID, err: err}
		}
		return markReadSuccessMsg{chatGUID: chatGUID}
	}
}

// deleteChatCmd and renameChatCmd need a backend that edits conversations.
// Slack has no equivalent, so the capability is asked for rather than assumed;
// a backend without it reports the same error as one that failed.
func deleteChatCmd(backend provider.Provider, chatGUID string) tea.Cmd {
	return func() tea.Msg {
		editor, ok := backend.(provider.ChatEditor)
		if !ok {
			return deleteChatErrMsg{chatGUID: chatGUID, err: errNoCapability(backend, "delete chats")}
		}
		if err := editor.DeleteChat(chatGUID); err != nil {
			return deleteChatErrMsg{chatGUID: chatGUID, err: err}
		}
		return deleteChatSuccessMsg{chatGUID: chatGUID}
	}
}

func renameChatCmd(backend provider.Provider, chatGUID, displayName string) tea.Cmd {
	return func() tea.Msg {
		editor, ok := backend.(provider.ChatEditor)
		if !ok {
			return renameChatErrMsg{chatGUID: chatGUID, displayName: displayName, err: errNoCapability(backend, "rename chats")}
		}
		if err := editor.RenameChat(chatGUID, displayName); err != nil {
			return renameChatErrMsg{chatGUID: chatGUID, displayName: displayName, err: err}
		}
		return renameChatSuccessMsg{chatGUID: chatGUID, displayName: displayName}
	}
}

func errNoBackend(chatGUID string) error {
	return fmt.Errorf("no backend configured for chat %s", chatGUID)
}

func errNoCapability(backend provider.Provider, what string) error {
	if backend == nil {
		return fmt.Errorf("no backend configured")
	}
	return fmt.Errorf("%s cannot %s", backend.ID(), what)
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
	if reaction, handled := parseReactionCommand(raw); handled {
		targetGUID := window.Messages.LatestMessageGUID()
		if targetGUID == "" {
			m.setAppError(fmt.Errorf("no message to react to"))
			return nil, true
		}
		window.Input.Clear()
		return sendReactionCmd(m.providerFor(window.Chat.GUID), window.Chat.GUID, targetGUID, reaction), true
	}

	if replyTo, text, handled, err := parseReplyCommand(raw); handled {
		if err != nil {
			m.setAppError(err)
			return nil, true
		}
		targetGUID, ok := window.Messages.MessageGUIDByNumber(replyTo)
		if !ok {
			m.setAppError(fmt.Errorf("no message #%d in this pane", replyTo))
			return nil, true
		}
		window.Input.Clear()
		pending := m.addPendingOutgoing(window.Chat.GUID, text)
		return tea.Batch(
			sendMessageCmd(m.providerFor(window.Chat.GUID), window.Chat.GUID, text, targetGUID, window.ID, pending.GUID),
			pendingTimeoutCmd(window.Chat.GUID, pending.GUID),
		), true
	}

	if target, handled, err := parseThreadCommand(raw); handled {
		if err != nil {
			m.setAppError(err)
			return nil, true
		}
		return m.openThread(window, target), true
	}

	if msgNum, linkNum, handled, err := parseOpenCommand(raw); handled {
		if err != nil {
			m.setAppError(err)
			return nil, true
		}
		rawURL, err := linkToOpen(window, msgNum, linkNum)
		if err != nil {
			m.setAppError(err)
			return nil, true
		}
		window.Input.Clear()
		return openURLCmd(rawURL), true
	}

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
	return openImageAttachmentCmd(m.providerFor(windowChatGUID(window)), att), true
}

// openThread shows the thread of message row n in a pane beside window. A
// thread already open in a pane is focused there instead of opened twice.
func (m *AppModel) openThread(window *ChatWindow, n int) tea.Cmd {
	messageGUID, ok := window.Messages.MessageGUIDByNumber(n)
	if !ok {
		m.setAppError(fmt.Errorf("no message #%d in this pane", n))
		return nil
	}
	backend := m.providerFor(window.Chat.GUID)
	threader, ok := backend.(provider.Threader)
	if !ok {
		m.setAppError(errNoCapability(backend, "open threads"))
		return nil
	}
	thread, err := threader.Thread(messageGUID)
	if err != nil {
		m.setAppError(err)
		return nil
	}
	window.Input.Clear()

	if showing := m.windowManager.ActiveWindowsShowingChat(thread.GUID); len(showing) > 0 {
		m.windowManager.SetFocus(showing[0].ID)
		m.saveLayoutState()
		return m.clearFocusedWindowNewMessageIndicator()
	}
	if err := m.windowManager.SplitWindow(SplitHorizontal); err != nil {
		return showToastCmd(capitalize(err.Error()), 3*time.Second)
	}
	m.updateLayout()
	return m.selectChat(&thread, m.windowManager.FocusedWindow())
}

// threadChat resolves a GUID the chat list does not have into the thread it
// names, so a pane that showed a thread shows it again after a restart.
func (m *AppModel) threadChat(chatGUID string) (*models.Chat, bool) {
	threader, ok := m.providerFor(chatGUID).(provider.Threader)
	if !ok {
		return nil, false
	}
	thread, err := threader.Thread(chatGUID)
	if err != nil || thread.GUID != chatGUID {
		return nil, false
	}
	return &thread, true
}

func parseReactionCommand(raw string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) != 1 {
		return "", false
	}
	switch parts[0] {
	case "/h":
		return "love", true
	case "/lol":
		return "laugh", true
	case "/tu":
		return "like", true
	case "/te":
		return "dislike", true
	case "/!!":
		return "emphasize", true
	case "/?":
		return "question", true
	default:
		return "", false
	}
}

// parseReplyCommand reads "/r #3 text goes here".
//
// On Slack this posts into the thread on message #3, which is the only way to
// answer in a thread rather than beside it; on iMessage it becomes a quoted
// reply. The #N numbering is the one already on screen with /img.
func parseReplyCommand(raw string) (target int, text string, handled bool, err error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "/r") {
		return 0, "", false, nil
	}
	parts := strings.Fields(s)
	if len(parts) == 0 || parts[0] != "/r" {
		return 0, "", false, nil
	}
	if len(parts) < 3 {
		return 0, "", true, fmt.Errorf("usage: /r #<message-number> <text>")
	}
	n, convErr := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
	if convErr != nil || n < 1 {
		return 0, "", true, fmt.Errorf("invalid message number: %s", parts[1])
	}
	// Everything after the number is the message, kept verbatim so its own
	// spacing survives.
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(s, "/r")), parts[1]))
	if body == "" {
		return 0, "", true, fmt.Errorf("usage: /r #<message-number> <text>")
	}
	return n, body, true, nil
}

// parseThreadCommand reads "/t #3": open the thread on message #3 in a pane of
// its own. The #N numbering is the one /r and /img use.
func parseThreadCommand(raw string) (target int, handled bool, err error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 || parts[0] != "/t" {
		return 0, false, nil
	}
	if len(parts) != 2 {
		return 0, true, fmt.Errorf("usage: /t #<message-number>")
	}
	n, convErr := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
	if convErr != nil || n < 1 {
		return 0, true, fmt.Errorf("invalid message number: %s", parts[1])
	}
	return n, true, nil
}

// parseOpenCommand reads "/open", "/open #3" and "/open #3 2". Without a
// message number it means the newest link in the pane; linkNum counts the
// links of message #msgNum in the order they are drawn, from 1.
func parseOpenCommand(raw string) (msgNum, linkNum int, handled bool, err error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 || parts[0] != "/open" {
		return 0, 0, false, nil
	}
	const usage = "usage: /open [#<message-number> [<link-number>]]"
	if len(parts) == 1 {
		return 0, 0, true, nil
	}
	if len(parts) > 3 {
		return 0, 0, true, errors.New(usage)
	}
	msgNum, convErr := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
	if convErr != nil || msgNum < 1 {
		return 0, 0, true, fmt.Errorf("invalid message number: %s", parts[1])
	}
	linkNum = 1
	if len(parts) == 3 {
		linkNum, convErr = strconv.Atoi(parts[2])
		if convErr != nil || linkNum < 1 {
			return 0, 0, true, fmt.Errorf("invalid link number: %s", parts[2])
		}
	}
	return msgNum, linkNum, true, nil
}

// linkToOpen resolves a parsed /open against what the pane shows.
func linkToOpen(window *ChatWindow, msgNum, linkNum int) (string, error) {
	if msgNum == 0 {
		link, ok := window.Messages.LatestLink()
		if !ok {
			return "", fmt.Errorf("no link in this pane")
		}
		return link, nil
	}
	links := window.Messages.LinksByNumber(msgNum)
	switch {
	case len(links) == 0:
		return "", fmt.Errorf("message #%d has no link", msgNum)
	case linkNum > len(links):
		return "", fmt.Errorf("message #%d has %d link(s), not %d", msgNum, len(links), linkNum)
	}
	return links[linkNum-1], nil
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

func openImageAttachmentCmd(backend provider.Provider, att models.Attachment) tea.Cmd {
	return func() tea.Msg {
		target := attachmentOpenTarget(att)
		if target == "" {
			store, ok := backend.(provider.AttachmentStore)
			if !ok || strings.TrimSpace(att.GUID) == "" {
				return errMsg{err: fmt.Errorf("image has no openable target")}
			}
			path, err := downloadAttachmentToTemp(store, att)
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

// attachmentOpenTarget returns a safe target to hand to the system opener.
// Only remote http(s) URLs are accepted; server-supplied file:// URLs and
// absolute local paths are deliberately rejected so a malicious or compromised
// server can't make us open arbitrary local files via the desktop handler.
// Anything not matched here falls back to downloading the attachment to a
// temp file (see openImageAttachmentCmd).
func attachmentOpenTarget(att models.Attachment) string {
	raw := strings.TrimSpace(att.URL)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return ""
}

func downloadAttachmentToTemp(store provider.AttachmentStore, att models.Attachment) (string, error) {
	data, mimeType, err := store.DownloadAttachment(att)
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
		if !isOpenableLink(rawURL) {
			return errMsg{err: fmt.Errorf("refusing to open %q: only http and https links open", rawURL)}
		}
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

func connectStreamCmd(stream provider.Stream) tea.Cmd {
	return func() tea.Msg {
		if err := stream.Connect(); err != nil {
			return wsConnectFailMsg{err: fmt.Errorf("realtime connection failed: %v", err)}
		}
		return wsConnectSuccessMsg{}
	}
}

// waitForStreamEvent blocks for the next realtime event. It yields no command
// when no stream is configured — the state every test runs in.
func (m AppModel) waitForStreamEvent() tea.Cmd {
	if m.stream == nil {
		return nil
	}
	return waitForStreamEventCmd(m.stream)
}

func waitForStreamEventCmd(stream provider.Stream) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-stream.Events()
		if !ok {
			return errMsg{err: fmt.Errorf("realtime connection closed")}
		}
		return providerEventMsg(event)
	}
}

func waitForStreamReconnectCmd(stream provider.Stream) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-stream.Reconnected()
		if !ok {
			return nil
		}
		return wsReconnectedMsg{}
	}
}

func waitForStreamDisconnectCmd(stream provider.Stream) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-stream.Disconnected()
		if !ok {
			return nil
		}
		return wsDisconnectedMsg{}
	}
}

func waitForStreamOverflowCmd(stream provider.Stream) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-stream.Overflowed()
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
//
// Only the tab on screen. Background tabs still take realtime events, and are
// caught up when they come back (afterTabChange); polling them too would
// multiply the calls by the number of tabs, against Slack's Tier 3 limit and
// Telegram's FLOOD_WAIT.
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
		cmds = append(cmds, prefetchMessagesCmd(m.providerFor(chatGUID), chatGUID, m.messageLimit))
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
		cmds = append(cmds, prefetchMessagesCmd(m.providerFor(chatGUID), chatGUID, m.messageLimit))
	}

	for _, window := range m.windowManager.WindowsInAllTabs() {
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

// handleProviderEvent processes a realtime update from any backend. The event
// is already normalized — parsing the backend's wire format happens in the
// provider, not here.
func (m *AppModel) handleProviderEvent(event provider.Event) (tea.Model, tea.Cmd) {
	switch event.Kind {
	case provider.EventNewMessage:
		msg := event.Message

		if msg.ChatGUID != "" {
			if len(messageDedupeKeys(msg)) == 0 {
				log.Printf("ignoring realtime new message without usable identity for chat %s", msg.ChatGUID)
				return m, m.waitForStreamEvent()
			}

			// Replace a local optimistic echo with the server-confirmed message.
			m.matchAndRemovePendingOutgoing(msg)

			// Cache the message (ignore duplicate realtime events by message identity).
			if !m.windowManager.CacheMessage(msg.ChatGUID, msg) {
				// A reaction or a duplicate. A reaction still has to reach the
				// open windows so tapbacks render immediately; a plain
				// duplicate is a no-op for AppendMessage.
				for _, window := range m.windowManager.WindowsShowingChat(msg.ChatGUID) {
					window.Messages.AppendMessage(msg)
				}
				return m, m.waitForStreamEvent()
			}
			// Intentionally do NOT refresh messageFetchedAt here. Only real
			// API fetches bump it so the TTL in shouldRefreshMessages can
			// still trigger a reconciling refetch and heal any hole that
			// WS may have left behind.
			m.saveMessageCacheChat(msg.ChatGUID)

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
			m.chatList.UpdateChatPreview(msg.ChatGUID, messagePreview(msg), msg.DateCreated)
		}

		cmds := []tea.Cmd{m.waitForStreamEvent()}
		if !msg.IsFromMe {
			if cmd := m.markFocusedChatRead(msg.ChatGUID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, m.announce(msg)...)
		if cmd := m.linkPreviewCmdsForMessages(msg.ChatGUID, []models.Message{msg}); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case provider.EventUpdatedMessage:
		// Reactions/tapbacks and edited bodies arrive as an update. Apply the
		// change to the cache and every open window without treating it as a
		// brand-new message: no unread marker, no chat-list reorder, no
		// preview, no link-preview fetch.
		msg := event.Message
		if msg.ChatGUID == "" || len(messageDedupeKeys(msg)) == 0 {
			return m, m.waitForStreamEvent()
		}
		// A reaction folds onto the cached target message; a duplicate is a
		// no-op. Either way the return value doesn't matter here.
		m.windowManager.CacheMessage(msg.ChatGUID, msg)
		for _, window := range m.windowManager.WindowsShowingChat(msg.ChatGUID) {
			window.Messages.AppendMessage(msg)
		}
		m.saveMessageCacheChat(msg.ChatGUID)
		return m, m.waitForStreamEvent()

	default:
		return m, m.waitForStreamEvent()
	}
}

// ShowError puts err in the status line the way a failed action would. It is
// for main to report a problem found before the program started, such as one
// network being unreachable while the others came up.
func (m *AppModel) ShowError(err error) {
	m.setAppError(err)
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
	// Panes are laid out from the top of the pane area, which is the top of
	// the screen unless the tab bar sits above it. This used to subtract one
	// row unconditionally, a leftover that made every pane click land on the
	// row above the pointer.
	contentY := msg.Y - m.tabBarRows()

	if mouseIsWheel(msg) {
		if contentY < 0 {
			return m, nil
		}
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
		if m.unreadBadgeAt(msg.X, msg.Y) {
			m.revealUnread()
			return m, nil
		}
		if m.showChatList && msg.X < m.chatListWidth {
			// Content starts after the panel's padding, and after its left
			// border too when the list was already the focused panel — the
			// layout the click landed on, not the one about to be drawn.
			contentX := msg.X - 1
			if m.focused == focusChatList {
				contentX--
			}
			if m.focused == focusWindow {
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Blur()
				}
			}
			m.focused = focusChatList
			m.updateLayout()
			// The list is drawn from the top row, so it takes the screen row
			// as it is. contentY is a row too high: every click landed on the
			// chat above the one under the pointer.
			chatGUID, _ := m.chatList.ChatAt(msg.Y)
			if m.chatList.ClickAt(contentX, msg.Y) {
				m.saveUIState()
			}
			if m.listDoubleClick(chatGUID) {
				return m, m.openSelectedChat()
			}
			return m, nil
		}

		relX := msg.X - m.panesX()
		if contentY < 0 {
			// The tab bar.
			if tab, ok := m.tabAt(relX); ok && m.windowManager.SwitchTab(tab) {
				return m, m.afterTabChange()
			}
			return m, nil
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
			// A link wins over the message's image: the click landed on the
			// link itself, while the image answers for every row of the message.
			if rawURL, ok := window.LinkAt(relX-window.x, localY); ok {
				return m, tea.Batch(cmd, openURLCmd(rawURL))
			}
			if att, ok := window.FirstImageAttachmentAtContentY(localY); ok {
				return m, tea.Batch(cmd, openImageAttachmentCmd(m.providerFor(windowChatGUID(window)), att))
			}
			return m, cmd
		}
	}

	return m, nil
}

// messagePreview is a message in a line, for the chat list and for notices:
// its text, or what it carries when it has none.
func messagePreview(msg models.Message) string {
	preview := strings.TrimSpace(displayText(msg))
	if preview == "" && len(msg.Attachments) > 0 {
		preview = attachmentPreviewFromMessage(msg)
	}
	return preview
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
	if !m.enableLinkPreviews || m.maxPreviewsPerMessage <= 0 {
		return nil
	}
	// Asked once for the whole batch: a backend that cannot resolve previews
	// should fetch nothing, not mark every link unavailable.
	previewer, ok := m.providerFor(chatGUID).(provider.LinkPreviewer)
	if !ok {
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
			cmds = append(cmds, loadLinkPreviewCmd(previewer, chatGUID, messageGUID, rawURL))
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

func loadLinkPreviewCmd(previewer provider.LinkPreviewer, chatGUID, messageGUID, rawURL string) tea.Cmd {
	return func() tea.Msg {
		preview, err := previewer.LinkPreview(rawURL)
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
			preview:     preview,
		}
	}
}
