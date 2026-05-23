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
	filteredIndices []int
	cursor          int
	offset          int // scroll offset (which item is at the top)
	width           int
	height          int
	focused         bool
	loading         bool
	showPreview     bool
	searchActive    bool
	searchQuery     string
	selectedStyle   lipgloss.Style
	normalStyle     lipgloss.Style
	newMessageStyle lipgloss.Style
	previewStyle    lipgloss.Style
	timeStyle       lipgloss.Style
	searchStyle     lipgloss.Style
}

func NewSimpleListModel() SimpleListModel {
	return SimpleListModel{
		cursor:      0,
		offset:      0,
		showPreview: true,
		selectedStyle: lipgloss.NewStyle().
			Foreground(ColorChatListSelectedForeground).
			Background(ColorChatListSelectedBackground),
		normalStyle: lipgloss.NewStyle(),
		newMessageStyle: lipgloss.NewStyle().
			Foreground(ColorChatListNewMessage),
		previewStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")),
		timeStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("242")),
		searchStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Italic(true),
	}
}

func (m *SimpleListModel) SetItems(chats []models.Chat) {
	m.items = chats
	m.rebuildFilter()
	m.clampCursor()
}

func (m *SimpleListModel) RemoveByGUID(chatGUID string) bool {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.items = append(m.items[:i], m.items[i+1:]...)
		m.rebuildFilter()
		m.clampCursor()
		return true
	}
	return false
}

func (m *SimpleListModel) SetLocalDisplayName(chatGUID, displayName string) bool {
	for i := range m.items {
		if m.items[i].GUID != chatGUID {
			continue
		}
		m.items[i].LocalDisplayName = strings.TrimSpace(displayName)
		m.rebuildFilter()
		return true
	}
	return false
}

func (m *SimpleListModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *SimpleListModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m *SimpleListModel) SetLoading(loading bool) {
	m.loading = loading
}

func (m *SimpleListModel) SetShowPreview(show bool) {
	m.showPreview = show
}

func (m *SimpleListModel) StartSearch() {
	m.searchActive = true
	m.searchQuery = ""
	m.rebuildFilter()
	m.cursor = 0
	m.offset = 0
}

func (m *SimpleListModel) ClearSearch() {
	m.searchActive = false
	m.searchQuery = ""
	m.rebuildFilter()
	m.clampCursor()
}

func (m *SimpleListModel) SearchActive() bool {
	return m.searchActive
}

func (m *SimpleListModel) AppendSearch(r rune) {
	if !m.searchActive {
		return
	}
	m.searchQuery += string(r)
	m.rebuildFilter()
	m.cursor = 0
	m.offset = 0
}

func (m *SimpleListModel) BackspaceSearch() {
	if !m.searchActive || m.searchQuery == "" {
		return
	}
	runes := []rune(m.searchQuery)
	m.searchQuery = string(runes[:len(runes)-1])
	m.rebuildFilter()
	m.clampCursor()
}

func (m *SimpleListModel) rebuildFilter() {
	if !m.searchActive || strings.TrimSpace(m.searchQuery) == "" {
		m.filteredIndices = make([]int, len(m.items))
		for i := range m.items {
			m.filteredIndices[i] = i
		}
		return
	}
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	m.filteredIndices = m.filteredIndices[:0]
	for i, chat := range m.items {
		if chatMatchesQuery(chat, q) {
			m.filteredIndices = append(m.filteredIndices, i)
		}
	}
}

func chatMatchesQuery(chat models.Chat, q string) bool {
	if strings.Contains(strings.ToLower(stripEmojis(chat.GetDisplayName())), q) {
		return true
	}
	if strings.Contains(strings.ToLower(chat.ChatIdentifier), q) {
		return true
	}
	for _, p := range chat.Participants {
		if strings.Contains(strings.ToLower(p.DisplayName), q) {
			return true
		}
		if strings.Contains(strings.ToLower(p.Address), q) {
			return true
		}
	}
	return false
}

func (m *SimpleListModel) clampCursor() {
	if len(m.filteredIndices) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= len(m.filteredIndices) {
		m.cursor = len(m.filteredIndices) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *SimpleListModel) visibleItemCapacity() int {
	reserved := 1 // title
	if m.searchActive {
		reserved++
	}
	lines := m.height - reserved
	linesPerItem := 1
	if m.showPreview {
		linesPerItem = 2
	}
	if lines < linesPerItem {
		return 0
	}
	return lines / linesPerItem
}

func (m *SimpleListModel) SelectedItem() *models.Chat {
	if m.cursor >= 0 && m.cursor < len(m.filteredIndices) {
		idx := m.filteredIndices[m.cursor]
		if idx >= 0 && idx < len(m.items) {
			return &m.items[idx]
		}
	}
	return nil
}

func (m *SimpleListModel) UpdateChatPreview(chatGUID, text string, when int64) {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.items[i].LastMessageText = text
		if when > 0 {
			m.items[i].LastMessageTime = when
		}
		return
	}
}

// SelectByGUID moves the list cursor to the chat with the given GUID.
func (m *SimpleListModel) SelectByGUID(chatGUID string) bool {
	for fi, idx := range m.filteredIndices {
		if m.items[idx].GUID != chatGUID {
			continue
		}
		m.cursor = fi
		visibleItems := m.visibleItemCapacity()
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

func (m *SimpleListModel) MarkNewMessage(chatGUID string) {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.items[i].HasNewMessage = true
		m.items[i].UnreadCount++
		if i > 0 {
			chat := m.items[i]
			copy(m.items[1:i+1], m.items[0:i])
			m.items[0] = chat
			m.rebuildFilter()
			oldIdx := i
			for fi, idx := range m.filteredIndices {
				if idx == 0 {
					if m.cursor == fi {
						m.cursor = 0
					} else if m.cursor < fi && oldIdx > 0 {
						// cursor stays on same chat if possible
					}
					break
				}
				if idx == oldIdx {
					if m.cursor == fi {
						m.cursor = 0
					}
					break
				}
			}
		}
		return
	}
}

func (m *SimpleListModel) ClickAt(y int) {
	titleRows := 1
	if m.searchActive {
		searchY := m.height - 1
		if y >= searchY {
			return
		}
	}
	itemY := y - titleRows
	if itemY < 0 {
		return
	}
	linesPerItem := 1
	if m.showPreview {
		linesPerItem = 2
	}
	itemIndex := itemY / linesPerItem
	idx := m.offset + itemIndex
	if idx >= 0 && idx < len(m.filteredIndices) {
		m.cursor = idx
	}
}

func (m *SimpleListModel) ClearNewMessage(chatGUID string) {
	for i, chat := range m.items {
		if chat.GUID == chatGUID {
			m.items[i].HasNewMessage = false
			m.items[i].UnreadCount = 0
			return
		}
	}
}

func (m *SimpleListModel) NewMessageCount() int {
	count := 0
	for _, chat := range m.items {
		if chat.HasNewMessage {
			count++
		}
	}
	return count
}

func chatListPrefix(chat models.Chat) string {
	if chat.IsGroup() {
		n := len(chat.Participants)
		if n > 1 {
			return "(" + itoa(n) + ") "
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func previewText(chat models.Chat) string {
	text := strings.TrimSpace(chat.LastMessageText)
	if text == "" && chat.LastMessage != nil {
		text = strings.TrimSpace(chat.LastMessage.Text)
	}
	if text == "" {
		if chat.HasNewMessage {
			return "New message"
		}
		return ""
	}
	return truncatePreview(text, 40)
}

func (m SimpleListModel) Update(msg tea.Msg) (SimpleListModel, tea.Cmd) {
	if m.searchActive {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.ClearSearch()
				return m, nil
			case "backspace", "ctrl+h":
				m.BackspaceSearch()
				return m, nil
			case "enter":
				m.ClearSearch()
				return m, nil
			default:
				if len(msg.Runes) == 1 {
					m.AppendSearch(msg.Runes[0])
				}
				return m, nil
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.filteredIndices)-1 {
				m.cursor++
				visibleItems := m.visibleItemCapacity()
				if visibleItems < 1 {
					visibleItems = 1
				}
				if m.cursor >= m.offset+visibleItems {
					m.offset = m.cursor - visibleItems + 1
				}
			}
		case "g":
			m.cursor = 0
			m.offset = 0
		case "G":
			m.cursor = len(m.filteredIndices) - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
			visibleItems := m.visibleItemCapacity()
			m.offset = max(0, len(m.filteredIndices)-visibleItems)
		}
	}
	return m, nil
}

func (m SimpleListModel) View() string {
	if m.loading && len(m.items) == 0 {
		return lipgloss.NewStyle().Foreground(ColorWindowPlaceholder).Render("Loading chats…")
	}
	if len(m.items) == 0 {
		return "No chats"
	}
	if len(m.filteredIndices) == 0 {
		var b strings.Builder
		b.WriteString(m.titleLine())
		b.WriteString("\n")
		b.WriteString(m.searchStyle.Render("  No matches"))
		if m.searchActive {
			b.WriteString("\n")
			b.WriteString(m.searchBar())
		}
		return b.String()
	}

	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteString("\n")

	visibleItems := m.visibleItemCapacity()
	end := min(m.offset+visibleItems, len(m.filteredIndices))
	nameWidth := m.width - 8
	if nameWidth < 8 {
		nameWidth = 8
	}

	for fi := m.offset; fi < end; fi++ {
		idx := m.filteredIndices[fi]
		chat := m.items[idx]
		displayName := chatListPrefix(chat) + stripEmojis(chat.GetDisplayName())

		linePrefix := ""
		if chat.HasNewMessage {
			linePrefix = "● "
		}
		lineSuffix := ""
		if chat.UnreadCount > 1 {
			lineSuffix = " (" + itoa(chat.UnreadCount) + ")"
		}

		lineWidth := m.width
		if lineWidth > 1 {
			lineWidth--
		}

		timeStr := ""
		timeStyled := m.timeStyle.Render(formatChatListTime(chat.LastMessageTime))
		reserved := 1 + lipgloss.Width(linePrefix) + lipgloss.Width(lineSuffix)
		if lipgloss.Width(timeStyled) > 0 && lineWidth-reserved-lipgloss.Width(timeStyled)-1 >= 1 {
			timeStr = timeStyled
			reserved += lipgloss.Width(timeStyled) + 1
		}
		nameWidth = lineWidth - reserved
		if nameWidth < 1 {
			nameWidth = 1
		}
		displayName = truncatePreview(displayName, nameWidth)

		line1 := linePrefix + displayName + lineSuffix
		if timeStr != "" {
			gap := lineWidth - lipgloss.Width(" "+line1) - lipgloss.Width(timeStr)
			if gap < 1 {
				gap = 1
			}
			timeStr = strings.Repeat(" ", gap) + timeStr
		}

		if fi == m.cursor {
			line1 = m.selectedStyle.Render(" " + line1 + timeStr)
		} else if chat.HasNewMessage {
			line1 = m.newMessageStyle.Render(" "+line1) + timeStr
		} else {
			line1 = m.normalStyle.Render(" "+line1) + timeStr
		}
		b.WriteString(line1)
		b.WriteString("\n")

		if m.showPreview {
			preview := previewText(chat)
			if preview != "" {
				preview = truncatePreview(preview, nameWidth)
				previewLine := m.previewStyle.Render("   " + preview)
				if fi == m.cursor {
					previewLine = m.selectedStyle.Render("   " + preview)
				}
				b.WriteString(previewLine)
			}
			b.WriteString("\n")
		}
	}

	if m.searchActive {
		b.WriteString(m.searchBar())
	}
	return b.String()
}

func (m SimpleListModel) titleLine() string {
	title := "CHATS"
	if m.focused {
		title = "▸ " + title
	}
	titleStyled := lipgloss.NewStyle().Bold(true).Render(title)
	if m.loading {
		titleStyled += lipgloss.NewStyle().Foreground(ColorWindowPlaceholder).Render(" …")
	}
	return titleStyled
}

func (m SimpleListModel) searchBar() string {
	q := m.searchQuery
	if q == "" {
		q = "_"
	}
	return m.searchStyle.Render("/" + q)
}
