package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/bluebubbles-tui/models"
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

func (m *ChatListModel) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width = width
	m.height = height
	m.list.SetSize(width, height)
}

func (m *ChatListModel) SelectedChat() *models.Chat {
	return m.list.SelectedItem()
}

// MarkNewMessage marks a chat as having a new message and moves it to the top
func (m *ChatListModel) MarkNewMessage(chatGUID string) {
	m.list.MarkNewMessage(chatGUID)
}

// ClickAt sets the cursor to the item at the given y-coordinate.
func (m *ChatListModel) ClickAt(y int) {
	m.list.ClickAt(y)
}

// ClearNewMessage clears the new message indicator for a chat
func (m *ChatListModel) ClearNewMessage(chatGUID string) {
	m.list.ClearNewMessage(chatGUID)
}

// SelectChatByGUID moves chat list selection to a specific chat if present.
func (m *ChatListModel) SelectChatByGUID(chatGUID string) bool {
	return m.list.SelectByGUID(chatGUID)
}

func (m ChatListModel) Update(msg tea.Msg) (ChatListModel, tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ChatListModel) View() string {
	return m.list.View()
}
