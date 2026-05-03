package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

// stripEmojis removes emoji and symbol characters from a string using an
// allowlist approach: only letters, digits, spaces, and common name punctuation
// are kept. This handles all emoji blocks without enumerating them.
func stripEmojis(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r): // keeps all scripts: Latin, Cyrillic, Arabic, CJK, etc.
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(r)
		case r == '-' || r == '\'' || r == '.' || r == ',' || r == '(' || r == ')':
			b.WriteRune(r)
			// skip everything else (emoji, symbols, variation selectors, ZWJ…)
		}
	}
	return strings.TrimSpace(b.String())
}

// SimpleListModel is a simple scrollable list without auto-centering
type SimpleListModel struct {
	items           []models.Chat
	cursor          int
	offset          int // scroll offset (which item is at the top)
	width           int
	height          int
	selectedStyle   lipgloss.Style
	normalStyle     lipgloss.Style
	newMessageStyle lipgloss.Style
}

func NewSimpleListModel() SimpleListModel {
	return SimpleListModel{
		cursor: 0,
		offset: 0,
		selectedStyle: lipgloss.NewStyle().
			Foreground(ColorChatListSelectedForeground).
			Background(ColorChatListSelectedBackground),
		normalStyle: lipgloss.NewStyle(),
		newMessageStyle: lipgloss.NewStyle().
			Foreground(ColorChatListNewMessage),
	}
}

func (m *SimpleListModel) SetItems(chats []models.Chat) {
	m.items = chats
	m.cursor = 0
	m.offset = 0
}

func (m *SimpleListModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *SimpleListModel) SelectedItem() *models.Chat {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return &m.items[m.cursor]
	}
	return nil
}

// SelectByGUID moves the list cursor to the chat with the given GUID.
// Returns true if found.
func (m *SimpleListModel) SelectByGUID(chatGUID string) bool {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.cursor = i
		visibleItems := m.height - 1 // -1 for title
		if visibleItems < 1 {
			visibleItems = 1
		}
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+visibleItems {
			m.offset = m.cursor - visibleItems + 1
		}
		return true
	}
	return false
}

// MarkNewMessage marks a chat as having a new message and moves it to the top
func (m *SimpleListModel) MarkNewMessage(chatGUID string) {
	for i, chat := range m.items {
		if chat.GUID == chatGUID {
			m.items[i].HasNewMessage = true
			m.items[i].UnreadCount++
			if i > 0 {
				// Move chat to top
				chat := m.items[i]
				copy(m.items[1:i+1], m.items[0:i])
				m.items[0] = chat
				// Adjust cursor if needed
				if m.cursor < i {
					m.cursor++
				} else if m.cursor == i {
					m.cursor = 0
				}
			}
			return
		}
	}
}

// ClickAt sets the cursor to the item at the given y-coordinate within the
// rendered list (y=0 is the title row, y=1 is the first item).
func (m *SimpleListModel) ClickAt(y int) {
	itemY := y - 1 // subtract title row
	if itemY < 0 {
		return
	}
	idx := m.offset + itemY
	if idx >= 0 && idx < len(m.items) {
		m.cursor = idx
	}
}

// ClearNewMessage clears the new message indicator for a chat
func (m *SimpleListModel) ClearNewMessage(chatGUID string) {
	for i, chat := range m.items {
		if chat.GUID == chatGUID {
			m.items[i].HasNewMessage = false
			m.items[i].UnreadCount = 0
			return
		}
	}
}

// NewMessageCount returns number of chats currently marked with new messages.
func (m *SimpleListModel) NewMessageCount() int {
	count := 0
	for _, chat := range m.items {
		if chat.HasNewMessage {
			count++
		}
	}
	return count
}

func (m SimpleListModel) Update(msg tea.Msg) (SimpleListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				// Scroll up if cursor goes above visible area
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				// Scroll down if cursor goes below visible area
				// Account for title (1 line)
				visibleItems := m.height - 1
				if m.cursor >= m.offset+visibleItems {
					m.offset = m.cursor - visibleItems + 1
				}
			}
		case "g":
			// Go to top
			m.cursor = 0
			m.offset = 0
		case "G":
			// Go to bottom
			m.cursor = len(m.items) - 1
			visibleItems := m.height - 1
			m.offset = max(0, len(m.items)-visibleItems)
		}
	}
	return m, nil
}

func (m SimpleListModel) View() string {
	if len(m.items) == 0 {
		return "No chats"
	}

	var b strings.Builder

	// Title
	title := lipgloss.NewStyle().Bold(true).Render("CHATS")
	b.WriteString(title)
	b.WriteString("\n")

	// Calculate visible range
	visibleItems := m.height - 1 // -1 for title
	end := min(m.offset+visibleItems, len(m.items))

	// Render visible items
	for i := m.offset; i < end; i++ {
		chat := m.items[i]
		name := stripEmojis(chat.GetDisplayName())

		// Truncate if too long
		maxWidth := m.width - 4 // Leave some padding
		if len([]rune(name)) > maxWidth {
			runes := []rune(name)
			name = string(runes[:maxWidth-1]) + "…"
		}

		// Add unread/new message indicator
		if chat.HasNewMessage {
			name = "● " + name
		}

		// Apply style
		if i == m.cursor {
			name = m.selectedStyle.Render(" " + name)
		} else if chat.HasNewMessage {
			name = m.newMessageStyle.Render(" " + name)
		} else {
			name = m.normalStyle.Render(" " + name)
		}

		b.WriteString(name)
		b.WriteString("\n")
	}

	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
