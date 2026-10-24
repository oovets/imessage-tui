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

func (m *ChatListModel) SetShowTime(show bool) {
	m.list.SetShowTime(show)
}

func (m *ChatListModel) SetNetworks(networkOf func(chatGUID string) string, order []string) {
	m.list.SetNetworks(networkOf, order)
}

func (m *ChatListModel) SetNetwork(network string) {
	m.list.SetNetwork(network)
}

func (m *ChatListModel) Network() string {
	return m.list.Network()
}

func (m *ChatListModel) CycleNetwork(step int) (string, bool) {
	return m.list.CycleNetwork(step)
}

func (m *ChatListModel) SetGroupByNetwork(group bool) {
	m.list.SetGroupByNetwork(group)
}

func (m *ChatListModel) GroupByNetwork() bool {
	return m.list.GroupByNetwork()
}

func (m *ChatListModel) NetworkCount() int {
	return m.list.NetworkCount()
}

func (m *ChatListModel) SetPinned(guids []string) {
	m.list.SetPinned(guids)
}

func (m *ChatListModel) Pinned() []string {
	return m.list.Pinned()
}

func (m *ChatListModel) IsPinned(chatGUID string) bool {
	return m.list.IsPinned(chatGUID)
}

func (m *ChatListModel) TogglePin(chatGUID string) bool {
	return m.list.TogglePin(chatGUID)
}

func (m *ChatListModel) MovePin(chatGUID string, step int) bool {
	return m.list.MovePin(chatGUID, step)
}

func (m *ChatListModel) SetIgnored(guids []string) {
	m.list.SetIgnored(guids)
}

func (m *ChatListModel) Ignored() []string {
	return m.list.Ignored()
}

func (m *ChatListModel) IsIgnored(chatGUID string) bool {
	return m.list.IsIgnored(chatGUID)
}

func (m *ChatListModel) ToggleIgnore(chatGUID string) bool {
	return m.list.ToggleIgnore(chatGUID)
}

func (m *ChatListModel) SetShowIgnored(show bool) {
	m.list.SetShowIgnored(show)
}

func (m *ChatListModel) ShowIgnored() bool {
	return m.list.ShowIgnored()
}

func (m *ChatListModel) SetShowUnknown(show bool) {
	m.list.SetShowUnknown(show)
}

func (m *ChatListModel) ShowUnknown() bool {
	return m.list.ShowUnknown()
}

func (m *ChatListModel) UnknownCount() int {
	return m.list.UnknownCount()
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

// ClickAt reports whether the click changed the network filter.
func (m *ChatListModel) ClickAt(x, y int) bool {
	return m.list.ClickAt(x, y)
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

func (m *ChatListModel) NewestUnread() *models.Chat {
	return m.list.NewestUnread()
}

func (m *ChatListModel) Listed(chatGUID string) bool {
	return m.list.Listed(chatGUID)
}

func (m *ChatListModel) Quiet(chatGUID string) bool {
	return m.list.Quiet(chatGUID)
}

func (m *ChatListModel) ChatByGUID(chatGUID string) (models.Chat, bool) {
	return m.list.ChatByGUID(chatGUID)
}

func (m ChatListModel) Update(msg tea.Msg) (ChatListModel, tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ChatListModel) View() string {
	return m.list.View()
}
