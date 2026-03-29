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

	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/config"
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

	showTimestamps  bool
	showLineNumbers bool
	showChatList    bool
}

func NewAppModel(client *api.Client, wsClient *ws.Client) AppModel {
	ui := config.LoadUIState()
	wm := NewWindowManager()
	wm.SetShowTimestamps(ui.ShowTimestamps)
	wm.SetShowLineNumbers(ui.ShowLineNumbers)
	return AppModel{
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

					if cmd, handled := m.handleLocalInputCommand(window, text); handled {
						return m, cmd
					}

					if strings.TrimSpace(text) != "" {
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

func (m *AppModel) saveUIState() {
	if err := config.SaveUIState(config.UIState{
		ShowTimestamps:  m.showTimestamps,
		ShowLineNumbers: m.showLineNumbers,
		ShowChatList:    m.showChatList,
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
			// Cache the message (ignore duplicate WS events by GUID).
			if !m.windowManager.CacheMessage(msg.ChatGUID, msg) {
				return m, waitForWSEventCmd(m.wsClient)
			}

			// Update ALL windows showing this chat
			windowsShowing := m.windowManager.WindowsShowingChat(msg.ChatGUID)
			for _, window := range windowsShowing {
				window.Messages.AppendMessage(msg)
			}

			// Local read/unread policy:
			// - If chat is currently shown in any window, clear indicator.
			// - If message is from us, don't create "new message" indicator.
			// - Otherwise mark as new in chat list.
			if len(windowsShowing) > 0 {
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
