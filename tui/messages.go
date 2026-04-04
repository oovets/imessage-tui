package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bluebubbles-tui/models"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type MessagesModel struct {
	viewport        viewport.Model
	messages        []models.Message
	messageGUIDs    map[string]struct{}
	chatName        string
	width           int
	height          int
	showTimestamps  bool
	showLineNumbers bool
	stickToBottom   bool
}

func NewMessagesModel() MessagesModel {
	vp := viewport.New(60, 15)
	vp.MouseWheelEnabled = true

	return MessagesModel{
		viewport:        vp,
		messageGUIDs:    make(map[string]struct{}),
		showTimestamps:  true,
		showLineNumbers: true,
		stickToBottom:   true,
	}
}

func (m *MessagesModel) SetMessages(messages []models.Message) {
	m.messages = messages
	m.rebuildGUIDIndex()
	m.renderContent()
}

// AppendMessage adds a single message to the list, deduplicating by GUID and keeping chronological order.
func (m *MessagesModel) AppendMessage(msg models.Message) {
	if msg.GUID != "" {
		if _, exists := m.messageGUIDs[msg.GUID]; exists {
			return
		}
		m.messageGUIDs[msg.GUID] = struct{}{}
	}

	if len(m.messages) == 0 || m.messages[len(m.messages)-1].DateCreated <= msg.DateCreated {
		// Fast path: most WS messages are newest.
		m.messages = append(m.messages, msg)
	} else {
		// Insert sorted by DateCreated.
		pos := sort.Search(len(m.messages), func(i int) bool {
			return m.messages[i].DateCreated > msg.DateCreated
		})
		m.messages = append(m.messages, models.Message{})
		copy(m.messages[pos+1:], m.messages[pos:])
		m.messages[pos] = msg
	}
	m.renderContent()
}

func (m *MessagesModel) SetChatName(name string) {
	m.chatName = stripEmojis(name)
}

func (m *MessagesModel) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width = width
	m.height = height
	m.viewport.Width = width
	// Reserve 1 line for the chat name header
	m.viewport.Height = height - 1
	m.renderContent()
}

func (m *MessagesModel) SetShowTimestamps(show bool) {
	if m.showTimestamps == show {
		return
	}
	m.showTimestamps = show
	m.renderContent()
}

func (m *MessagesModel) SetShowLineNumbers(show bool) {
	if m.showLineNumbers == show {
		return
	}
	m.showLineNumbers = show
	m.renderContent()
}

// FirstImageAttachmentByNumber returns the first image attachment for message #N
// where N is 1-based according to the rendered message list.
func (m *MessagesModel) FirstImageAttachmentByNumber(n int) (models.Attachment, bool) {
	if n < 1 || n > len(m.messages) {
		return models.Attachment{}, false
	}
	msg := m.messages[n-1]
	for _, att := range msg.Attachments {
		if isImageAttachment(att) {
			return att, true
		}
	}
	return models.Attachment{}, false
}

func (m *MessagesModel) renderContent() {
	if len(m.messages) == 0 {
		m.viewport.SetContent("(No messages yet)")
		return
	}

	wrapWidth := m.width
	if wrapWidth < 1 {
		wrapWidth = 60
	}

	var sb strings.Builder

	for i, msg := range m.messages {
		timeStr := msg.ParsedTime().Format("15:04")

		var sender string
		if msg.IsFromMe {
			sender = "You"
		} else if msg.Handle != nil && msg.Handle.DisplayName != "" {
			sender = stripEmojis(msg.Handle.DisplayName)
		} else if msg.Handle != nil {
			sender = msg.Handle.Address
		} else {
			sender = "Unknown"
		}

		prefix := ""
		if m.showTimestamps {
			prefix = timeStr + " "
		}

		body := strings.TrimSpace(msg.Text)
		if hasImageAttachment(msg) {
			if body == "" {
				body = "[IMG]"
			} else {
				body += " [IMG]"
			}
		}
		lineNum := ""
		if m.showLineNumbers {
			lineNum = fmt.Sprintf("#%d ", i+1)
		}
		fullText := fmt.Sprintf("%s%s%s:", prefix, lineNum, sender)
		if body != "" {
			fullText += " " + body
		}

		if msg.IsFromMe {
			// Wrap to wrapWidth, then manually right-align each line.
			// Using Align(Right)+Width together makes each wrapped line get
			// padded independently, which looks wrong for short continuation lines.
			wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(fullText)
			for i, line := range strings.Split(wrapped, "\n") {
				if i > 0 {
					sb.WriteString("\n")
				}
				content := strings.TrimRight(line, " ")
				if padLen := wrapWidth - lipgloss.Width(content); padLen > 0 {
					sb.WriteString(strings.Repeat(" ", padLen))
				}
				sb.WriteString(MyMessageStyle.Render(content))
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString(TheirMessageStyle.Width(wrapWidth).Render(fullText))
			sb.WriteString("\n")
		}
	}

	m.viewport.SetContent(sb.String())
	if m.stickToBottom {
		m.viewport.GotoBottom()
	}
}

func (m *MessagesModel) rebuildGUIDIndex() {
	m.messageGUIDs = make(map[string]struct{}, len(m.messages))
	for _, msg := range m.messages {
		if msg.GUID != "" {
			m.messageGUIDs[msg.GUID] = struct{}{}
		}
	}
}

func hasImageAttachment(msg models.Message) bool {
	for _, att := range msg.Attachments {
		if isImageAttachment(att) {
			return true
		}
	}
	return false
}

func isImageAttachment(att models.Attachment) bool {
	mime := strings.ToLower(strings.TrimSpace(att.MimeType))
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	for _, raw := range []string{att.FileName, att.URL, att.Path, att.PathOnDisk} {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		switch {
		case strings.Contains(s, ".jpg"),
			strings.Contains(s, ".jpeg"),
			strings.Contains(s, ".png"),
			strings.Contains(s, ".gif"),
			strings.Contains(s, ".webp"),
			strings.Contains(s, ".bmp"),
			strings.Contains(s, ".heic"),
			strings.Contains(s, ".heif"):
			return true
		}
	}
	return false
}

func (m *MessagesModel) ScrollUp() {
	m.viewport.LineUp(3)
	m.stickToBottom = m.viewport.AtBottom()
}

func (m *MessagesModel) ScrollDown() {
	m.viewport.LineDown(3)
	m.stickToBottom = m.viewport.AtBottom()
}

func (m MessagesModel) Update(msg tea.Msg) (MessagesModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	m.stickToBottom = m.viewport.AtBottom()
	return m, cmd
}

func (m MessagesModel) View() string {
	header := ""
	if m.chatName != "" {
		header = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Render(m.chatName) + "\n"
	}

	return header + m.viewport.View()
}
