package tui

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/models"
	"github.com/bluebubbles-tui/ws"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	sendSuccessMsg      struct{ windowID WindowID }
	sendErrMsg          struct{ err error }
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

	showTimestamps bool
	showChatList   bool
}

func NewAppModel(client *api.Client, wsClient *ws.Client) AppModel {
	return AppModel{
		chatList:       NewChatListModel(),
		windowManager:  NewWindowManager(),
		apiClient:      client,
		wsClient:       wsClient,
		focused:        focusChatList,
		width:          80,
		height:         24,
		showTimestamps: true,
		showChatList:   true,
	}
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
		m.updateLayout()
		// Auto-select first chat in focused window if available
		if len(msg) > 0 {
			window := m.windowManager.FocusedWindow()
			if window != nil {
				chat := msg[0]
				window.SetChat(&chat)
				m.focused = focusWindow
				window.Input.textarea.Focus()
				return m, loadMessagesCmd(m.apiClient, chat.GUID, window.ID)
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
		for _, window := range m.windowManager.WindowsShowingChat(msg.chatGUID) {
			window.Messages.SetMessages(merged)
		}
		return m, nil

	case sendSuccessMsg:
		// Clear input for the window that sent
		if window := m.windowManager.windows[msg.windowID]; window != nil {
			window.Input.Clear()
			if window.Chat != nil {
				return m, loadMessagesCmd(m.apiClient, window.Chat.GUID, window.ID)
			}
		}
		return m, nil

	case sendErrMsg:
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
			return m, tea.Quit

		// Split operations
		case "ctrl+f":
			// Split horizontal (side by side)
			m.windowManager.SplitWindow(SplitHorizontal)
			m.updateLayout()
			return m, nil

		case "ctrl+g":
			// Split vertical (stacked)
			m.windowManager.SplitWindow(SplitVertical)
			m.updateLayout()
			return m, nil

		case "ctrl+w":
			// Close focused window
			m.windowManager.CloseWindow()
			m.updateLayout()
			return m, nil

		case "ctrl+s":
			// Toggle chat list visibility
			m.showChatList = !m.showChatList
			if !m.showChatList && m.focused == focusChatList {
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
			}
			m.updateLayout()
			return m, nil

		case "ctrl+t":
			// Toggle timestamps
			m.showTimestamps = !m.showTimestamps
			m.windowManager.SetShowTimestamps(m.showTimestamps)
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
				}
			} else {
				// From chat list → go to focused window
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
			}
			return m, nil

		case "right":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirRight)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
				}
			} else {
				// From chat list → go to focused window
				m.focused = focusWindow
				if window := m.windowManager.FocusedWindow(); window != nil {
					window.Input.textarea.Focus()
				}
			}
			return m, nil

		case "ctrl+up":
			if m.focused == focusWindow {
				before := m.windowManager.FocusedWindow()
				m.windowManager.FocusDirection(DirUp)
				after := m.windowManager.FocusedWindow()
				if before != after {
					after.Input.textarea.Focus()
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
						window.SetChat(selected)
						m.chatList.ClearNewMessage(selected.GUID)
						// Switch focus to window input
						m.focused = focusWindow
						window.Input.textarea.Focus()
						return m, loadMessagesCmd(m.apiClient, selected.GUID, window.ID)
					}
				}
				return m, nil
			} else if m.focused == focusWindow {
				// Send message from focused window
				window := m.windowManager.FocusedWindow()
				if window != nil && window.Chat != nil {
					text := window.Input.GetText()
					if text != "" {
						return m, sendMessageCmd(m.apiClient, window.Chat.GUID, text, window.ID)
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
	windowsHeight := m.height

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
		messages, err := client.GetMessages(chatGUID, 50)
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load messages: %v", err)}
		}
		return messagesLoadedMsg{chatGUID: chatGUID, messages: messages}
	}
}

func sendMessageCmd(client *api.Client, chatGUID, text string, windowID WindowID) tea.Cmd {
	return func() tea.Msg {
		if err := client.SendMessage(chatGUID, text, ""); err != nil {
			return sendErrMsg{err: err}
		}
		return sendSuccessMsg{windowID: windowID}
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
			Chats []struct {
				GUID string `json:"guid"`
			} `json:"chats"`
		}
		if err := json.Unmarshal(event.Data, &wsMsg); err != nil {
			return m, waitForWSEventCmd(m.wsClient)
		}

		msg := wsMsg.Message
		if len(wsMsg.Chats) > 0 {
			msg.ChatGUID = wsMsg.Chats[0].GUID
		}

		if msg.ChatGUID != "" {
			// Cache the message
			m.windowManager.CacheMessage(msg.ChatGUID, msg)

			// Update ALL windows showing this chat
			windowsShowing := m.windowManager.WindowsShowingChat(msg.ChatGUID)
			for _, window := range windowsShowing {
				window.Messages.AppendMessage(msg)
			}

			// If no window is showing this chat, mark in chat list
			if len(windowsShowing) == 0 {
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
