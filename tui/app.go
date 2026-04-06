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

	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/ws"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const initialMessageFetchLimit = 50
const messageCacheRefreshTTL = 20 * time.Second
const initialPrefetchChatCount = 5

type focusRegion int

const (
	focusChatList focusRegion = iota
	focusWindow
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
	errMsg              struct{ err error }
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

	showTimestamps  bool
	showLineNumbers bool
	showChatList    bool
	showSenderNames bool

	messageFetchInFlight map[string]bool
	messageFetchedAt     map[string]time.Time
	pendingOutgoing      map[string][]models.Message
	savedLayoutState     *config.LayoutState
	didRestoreLayout     bool
}

func NewAppModel(client *api.Client, wsClient *ws.Client) AppModel {
	ui := config.LoadUIState()
	wm := NewWindowManager()
	wm.SetShowTimestamps(ui.ShowTimestamps)
	wm.SetShowLineNumbers(ui.ShowLineNumbers)
	wm.SetShowSenderNames(ui.ShowSenderNames)
	app := AppModel{
		chatList:        NewChatListModel(),
		windowManager:   wm,
		apiClient:       client,
		wsClient:        wsClient,
		focused:         focusChatList,
		width:           80,
		height:          24,
		showTimestamps:  ui.ShowTimestamps,
		showLineNumbers: ui.ShowLineNumbers,
		showChatList:    ui.ShowChatList,
		showSenderNames: ui.ShowSenderNames,
		messageFetchInFlight: make(map[string]bool),
		messageFetchedAt:     make(map[string]time.Time),
		pendingOutgoing:      make(map[string][]models.Message),
	}
	if layoutState, ok := config.LoadLayoutState(); ok {
		app.savedLayoutState = &layoutState
	}
	app.restoreMessageCache()
	return app
}

func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		loadChatsCmd(m.apiClient),
	}

	// Try to connect WebSocket for real-time updates
	if m.wsClient != nil {
		cmds = append(cmds, connectWSCmd(m.wsClient))
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
		m.chatList.SetChats([]models.Chat(msg))
		if m.restoreLayoutFromState([]models.Chat(msg)) {
			m.updateLayout()
			return m, nil
		}
		m.updateLayout()
		var cmds []tea.Cmd
		// Auto-select first chat in focused window if available
		if len(msg) > 0 {
			window := m.windowManager.FocusedWindow()
			if window != nil {
				chat := msg[0]
				m.focused = focusWindow
				window.Input.textarea.Focus()
				if cmd := m.selectChat(&chat, window); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if cmd := m.prefetchTopChatsCmd([]models.Chat(msg), chat.GUID); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
			if cmd := m.prefetchTopChatsCmd([]models.Chat(msg), ""); cmd != nil {
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
		merged := msg.messages
		if len(merged) > 0 {
			newestAPITime := merged[len(merged)-1].DateCreated
			for _, cached := range m.windowManager.GetCachedMessages(msg.chatGUID) {
				if cached.DateCreated <= newestAPITime {
					continue
				}
				// Only add if not already present
				found := false
				for _, m := range merged {
					if m.GUID == cached.GUID {
						found = true
						break
					}
				}
				if !found {
					merged = append(merged, cached)
				}
			}
		}
		m.windowManager.SetCachedMessages(msg.chatGUID, merged)
		m.messageFetchInFlight[msg.chatGUID] = false
		m.messageFetchedAt[msg.chatGUID] = time.Now()
		m.saveMessageCache()
		for _, window := range m.windowManager.WindowsShowingChat(msg.chatGUID) {
			window.Messages.SetMessages(merged)
		}
		return m, nil

	case loadMessagesErrMsg:
		m.messageFetchInFlight[msg.chatGUID] = false
		if msg.report {
			m.err = msg.err
		}
		return m, nil

	case sendSuccessMsg:
		// Clear input for the window that sent
		if window := m.windowManager.windows[msg.windowID]; window != nil {
			window.Input.Clear()
		}
		// Keep optimistic message in place; WS delivery replaces it with server GUID.
		return m, nil

	case sendErrMsg:
		m.removePendingOutgoingByGUID(msg.chatGUID, msg.pendingGUID)
		m.err = msg.err
		return m, nil

	case wsConnectSuccessMsg:
		m.wsConnected = true
		return m, waitForWSEventCmd(m.wsClient)

	case wsConnectFailMsg:
		m.err = msg.err
		return m, nil

	case wsEventMsg:
		return m.handleWSEvent(models.WSEvent(msg))

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.MouseMsg:
		// Only handle left-click for focus/navigation; let other events
		// (scroll wheel) fall through to the focused component.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.showChatList && msg.X < ChatListWidth {
				// Click in chat list — focus it and move cursor to clicked item
				if m.focused == focusWindow {
					if window := m.windowManager.FocusedWindow(); window != nil {
						window.Input.textarea.Blur()
					}
				}
				m.focused = focusChatList
				m.chatList.ClickAt(msg.Y)
			} else {
				// Click in windows area — find and focus the clicked window
				relX := msg.X
				if m.showChatList {
					relX = msg.X - ChatListWidth
				}
				for _, window := range m.windowManager.AllWindows() {
					if relX >= window.x && relX < window.x+window.width &&
						msg.Y >= window.y && msg.Y < window.y+window.height {
						if old := m.windowManager.FocusedWindow(); old != nil && old.ID != window.ID {
							old.Input.textarea.Blur()
						}
						m.windowManager.SetFocus(window.ID)
						window.Input.textarea.Focus()
						m.focused = focusWindow
						m.clearFocusedWindowNewMessageIndicator()
						m.saveLayoutState()
						break
					}
				}
			}
			return m, nil
		}

	case tea.KeyMsg:
		m.lastKey = msg.String()
		// Handle global keys first
		switch msg.String() {
		case "q", "ctrl+c":
			m.saveMessageCache()
			m.saveLayoutState()
			return m, tea.Quit

		// Split operations
		case "ctrl+f":
			// Split horizontal (side by side)
			m.windowManager.SplitWindow(SplitHorizontal)
			m.updateLayout()
			m.saveLayoutState()
			return m, nil

		case "ctrl+g":
			// Split vertical (stacked)
			m.windowManager.SplitWindow(SplitVertical)
			m.updateLayout()
			m.saveLayoutState()
			return m, nil

		case "ctrl+w":
			// Close focused window
			m.windowManager.CloseWindow()
			m.clearFocusedWindowNewMessageIndicator()
			m.updateLayout()
			m.saveLayoutState()
			return m, nil

		case "ctrl+s":
			// Toggle chat list visibility
			m.showChatList = !m.showChatList
			if !m.showChatList && m.focused == focusChatList {
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				m.clearFocusedWindowNewMessageIndicator()
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
					m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
				}
			} else {
				// From chat list → go to focused window
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				m.clearFocusedWindowNewMessageIndicator()
				m.saveLayoutState()
			}
			return m, nil

		case "right":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirRight)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
				}
			} else {
				// From chat list → go to focused window
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
				m.clearFocusedWindowNewMessageIndicator()
				m.saveLayoutState()
			}
			return m, nil

		case "ctrl+up":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirUp)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
					m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
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
					m.clearFocusedWindowNewMessageIndicator()
					m.saveLayoutState()
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
				m.clearFocusedWindowNewMessageIndicator()
				m.saveLayoutState()
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
						return m, sendMessageCmd(m.apiClient, window.Chat.GUID, text, window.ID, pending.GUID)
					}
				}
				return m, nil
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
	if err := config.SaveUIState(config.UIState{
		ShowTimestamps:  m.showTimestamps,
		ShowLineNumbers: m.showLineNumbers,
		ShowChatList:    m.showChatList,
		ShowSenderNames: m.showSenderNames,
	}); err != nil {
		log.Printf("failed to save ui state: %v", err)
	}
}

func (m *AppModel) updateLayout() {
	// Calculate chat list dimensions (no borders, just padding)
	chatListContentHeight := m.height
	chatListWidth := 0
	if m.showChatList {
		chatListWidth = ChatListWidth
	}
	m.chatList.SetSize(chatListWidth, chatListContentHeight)

	// Calculate window area (everything to the right of chat list)
	windowsWidth := m.width - 2 // -2 for padding
	if m.showChatList {
		windowsWidth -= ChatListWidth
	}
	if windowsWidth < 1 {
		windowsWidth = 1
	}
	windowsHeight := m.height
	if windowsHeight < 1 {
		windowsHeight = 1
	}

	m.windowManager.SetSize(windowsWidth, windowsHeight)
}

func (m AppModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Render chat list panel
	chatPanel := ""
	if m.showChatList {
		chatListStyle := PanelStyle
		if m.focused == focusChatList {
			chatListStyle = ActivePanelStyle
		}
		panelHeight := m.height
		chatPanel = chatListStyle.
			Width(ChatListWidth).
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

	// Render status bar
	return content
}

// Command constructors

func loadChatsCmd(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats(50)
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load chats: %v", err)}
		}
		return chatsLoadedMsg(chats)
	}
}

func loadMessagesCmd(client *api.Client, chatGUID string, windowID WindowID) tea.Cmd {
	return func() tea.Msg {
		messages, err := client.GetMessages(chatGUID, initialMessageFetchLimit)
		if err != nil {
			return loadMessagesErrMsg{chatGUID: chatGUID, err: fmt.Errorf("failed to load messages: %v", err), report: true}
		}
		return messagesLoadedMsg{chatGUID: chatGUID, messages: messages}
	}
}

func prefetchMessagesCmd(client *api.Client, chatGUID string) tea.Cmd {
	return func() tea.Msg {
		messages, err := client.GetMessages(chatGUID, initialMessageFetchLimit)
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

	if !m.shouldRefreshMessages(chat.GUID) {
		return nil
	}

	m.messageFetchInFlight[chat.GUID] = true
	return loadMessagesCmd(m.apiClient, chat.GUID, window.ID)
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
	if len(snapshot) == 0 {
		return
	}

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

	if err := config.SaveMessageCache(state); err != nil {
		log.Printf("failed to save message cache: %v", err)
	}
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
	if err := config.SaveLayoutState(state); err != nil {
		log.Printf("failed to save layout state: %v", err)
	}
}

func (m *AppModel) clearFocusedWindowNewMessageIndicator() {
	if m.focused != focusWindow {
		return
	}
	window := m.windowManager.FocusedWindow()
	if window == nil || window.Chat == nil {
		return
	}
	m.chatList.SelectChatByGUID(window.Chat.GUID)
	m.chatList.ClearNewMessage(window.Chat.GUID)
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
		cmds = append(cmds, prefetchMessagesCmd(m.apiClient, chatGUID))
	}

	if len(cmds) == 0 {
		return nil
	}

	return tea.Batch(cmds...)
}

func sendMessageCmd(client *api.Client, chatGUID, text string, windowID WindowID, pendingGUID string) tea.Cmd {
	return func() tea.Msg {
		if err := client.SendMessage(chatGUID, text, ""); err != nil {
			return sendErrMsg{err: err, chatGUID: chatGUID, pendingGUID: pendingGUID}
		}
		return sendSuccessMsg{windowID: windowID}
	}
}

func (m *AppModel) addPendingOutgoing(chatGUID, text string) models.Message {
	pending := models.Message{
		GUID:        fmt.Sprintf("local-%d", time.Now().UnixNano()),
		Text:        text,
		IsFromMe:    true,
		DateCreated: time.Now().UnixMilli(),
		ChatGUID:    chatGUID,
	}

	m.pendingOutgoing[chatGUID] = append(m.pendingOutgoing[chatGUID], pending)
	for _, window := range m.windowManager.WindowsShowingChat(chatGUID) {
		window.Messages.AppendMessage(pending)
	}
	return pending
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
	for i, p := range pending {
		if strings.TrimSpace(p.Text) != body {
			continue
		}
		// Guard against accidental match to old pending entry.
		diff := msg.DateCreated - p.DateCreated
		if diff < 0 {
			diff = -diff
		}
		if diff <= 2*60*1000 {
			matchIdx = i
			break
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
		m.err = err
		return nil, true
	}

	att, ok := window.Messages.FirstImageAttachmentByNumber(msgNum)
	if !ok {
		m.err = fmt.Errorf("message #%d has no image attachment", msgNum)
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

// handleWSEvent processes incoming WebSocket events
func (m *AppModel) handleWSEvent(event models.WSEvent) (tea.Model, tea.Cmd) {
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
			return m, waitForWSEventCmd(m.wsClient)
		}

		msg := wsMsg.Message
		if len(wsMsg.Chats) > 0 {
			msg.ChatGUID = wsMsg.Chats[0].GUID
		} else if msg.ChatGUID == "" {
			msg.ChatGUID = wsMsg.ChatGUID
		}

		if msg.ChatGUID != "" {
			// Replace a local optimistic echo with the server-confirmed message.
			m.matchAndRemovePendingOutgoing(msg)

			// Cache the message (ignore duplicate WS events by GUID).
			if !m.windowManager.CacheMessage(msg.ChatGUID, msg) {
				return m, waitForWSEventCmd(m.wsClient)
			}
			m.messageFetchedAt[msg.ChatGUID] = time.Now()
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
		}

		return m, waitForWSEventCmd(m.wsClient)

	case "updated-message":
		return m, waitForWSEventCmd(m.wsClient)

	case "chat-read-status-changed":
		return m, waitForWSEventCmd(m.wsClient)

	default:
		return m, waitForWSEventCmd(m.wsClient)
	}
}
