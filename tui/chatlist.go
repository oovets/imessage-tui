package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
)

type ChatListModel struct {
	list   SimpleListModel
	chats  []models.Chat
	width  int
	height int
}

func NewChatListModel() ChatListModel {
	return ChatListModel{
		list: NewSimpleListModel(),
	}
}

func (m *ChatListModel) SetChats(chats []models.Chat) {
	m.chats = chats
	m.list.SetItems(chats)
}

func (m *ChatListModel) RemoveChatByGUID(chatGUID string) bool {
	removed := false
	for i, chat := range m.chats {
		if chat.GUID != chatGUID {
			continue
		}
		m.chats = append(m.chats[:i], m.chats[i+1:]...)
		removed = true
		break
	}
	if m.list.RemoveByGUID(chatGUID) {
		removed = true
	}
	return removed
}

func (m *ChatListModel) SetLocalDisplayName(chatGUID, displayName string) bool {
	updated := false
	for i := range m.chats {
		if m.chats[i].GUID != chatGUID {
			continue
		}
		m.chats[i].LocalDisplayName = displayName
		updated = true
		break
	}
	if m.list.SetLocalDisplayName(chatGUID, displayName) {
		updated = true
	}
	return updated
}

func (m *ChatListModel) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width = width
	m.height = height
	m.list.SetSize(width, height)
}

func (m *ChatListModel) SetFocused(focused bool) {
	m.list.SetFocused(focused)
}

func (m *ChatListModel) SetLoading(loading bool) {
	m.list.SetLoading(loading)
}

func (m *ChatListModel) SetShowPreview(show bool) {
	m.list.SetShowPreview(show)
}

func (m *ChatListModel) StartSearch() {
	m.list.StartSearch()
}

func (m *ChatListModel) ClearSearch() {
	m.list.ClearSearch()
}

func (m *ChatListModel) SearchActive() bool {
	return m.list.SearchActive()
}

func (m *ChatListModel) UpdateChatPreview(chatGUID, text string, when int64) {
	m.list.UpdateChatPreview(chatGUID, text, when)
}

func (m *ChatListModel) SelectedChat() *models.Chat {
	return m.list.SelectedItem()
}

func (m *ChatListModel) MarkNewMessage(chatGUID string) {
	m.list.MarkNewMessage(chatGUID)
}

func (m *ChatListModel) ClickAt(y int) {
	m.list.ClickAt(y)
}

func (m *ChatListModel) ClearNewMessage(chatGUID string) {
	m.list.ClearNewMessage(chatGUID)
}

func (m *ChatListModel) SelectChatByGUID(chatGUID string) bool {
	return m.list.SelectByGUID(chatGUID)
}

func (m *ChatListModel) NewMessageCount() int {
	return m.list.NewMessageCount()
}

func (m ChatListModel) Update(msg tea.Msg) (ChatListModel, tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ChatListModel) View() string {
	return m.list.View()
}
