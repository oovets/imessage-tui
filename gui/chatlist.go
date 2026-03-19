package gui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

// ChatList renders the left-side conversation list split into Chats / Groups sections.
// All methods must be called from the Fyne main goroutine.
type ChatList struct {
	scroll       *container.Scroll
	vbox         *fyne.Container
	chats        []models.Chat
	selectedGUID string
	onSelect     func(*models.Chat)
	onRename     func(chatGUID string) // called after alias save/clear
}

// SetSelected marks a chat as active and rebuilds the list to show the indicator.
func (cl *ChatList) SetSelected(guid string) {
	cl.selectedGUID = guid
	cl.rebuildVBox()
}

func NewChatList(onSelect func(*models.Chat)) *ChatList {
	cl := &ChatList{onSelect: onSelect}
	cl.vbox = container.NewVBox()
	cl.scroll = container.NewVScroll(cl.vbox)
	return cl
}

// UnreadChatsCount returns how many chats currently have unread/new state.
func (cl *ChatList) UnreadChatsCount() int {
	count := 0
	for _, c := range cl.chats {
		if c.HasNewMessage || c.UnreadCount > 0 {
			count++
		}
	}
	return count
}

// Widget returns the Fyne canvas object for embedding in layouts.
func (cl *ChatList) Widget() fyne.CanvasObject {
	return cl.scroll
}

// SetChats replaces the full chat list. Chats should be sorted by last activity descending.
func (cl *ChatList) SetChats(chats []models.Chat) {
	cl.chats = make([]models.Chat, len(chats))
	copy(cl.chats, chats)
	cl.rebuildVBox()
}

// MarkNewMessage moves the chat to the top of its section and shows the new-message dot.
func (cl *ChatList) MarkNewMessage(chatGUID string) {
	for i, chat := range cl.chats {
		if chat.GUID != chatGUID {
			continue
		}
		cl.chats[i].HasNewMessage = true

		isGroup := cl.chats[i].IsGroup()
		sectionStart := 0
		for j, c := range cl.chats {
			if c.IsGroup() == isGroup {
				sectionStart = j
				break
			}
		}
		if i > sectionStart {
			moving := cl.chats[i]
			copy(cl.chats[sectionStart+1:i+1], cl.chats[sectionStart:i])
			cl.chats[sectionStart] = moving
		}
		cl.rebuildVBox()
		return
	}
}

// ClearNewMessage clears the new-message indicator.
func (cl *ChatList) ClearNewMessage(chatGUID string) {
	for i, chat := range cl.chats {
		if chat.GUID == chatGUID {
			cl.chats[i].HasNewMessage = false
			cl.rebuildVBox()
			return
		}
	}
}

// rebuildVBox rebuilds the VBox content from cl.chats.
func (cl *ChatList) rebuildVBox() {
	cl.vbox.Objects = nil

	var directIdxs, groupIdxs []int
	for i, c := range cl.chats {
		if c.IsGroup() {
			groupIdxs = append(groupIdxs, i)
		} else {
			directIdxs = append(directIdxs, i)
		}
	}

	if len(directIdxs) > 0 {
		cl.vbox.Add(newSectionHeader("Chats"))
		for _, idx := range directIdxs {
			cl.vbox.Add(newChatItemWidget(cl.chats[idx], cl, cl.selectedGUID == cl.chats[idx].GUID))
		}
	}
	if len(groupIdxs) > 0 {
		cl.vbox.Add(newSectionHeader("Groups"))
		for _, idx := range groupIdxs {
			cl.vbox.Add(newChatItemWidget(cl.chats[idx], cl, cl.selectedGUID == cl.chats[idx].GUID))
		}
	}

	cl.vbox.Refresh()
}

func newSectionHeader(title string) fyne.CanvasObject {
	l := widget.NewLabel(title)
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.Importance = widget.LowImportance
	return l
}

// showRenameDialog opens an input dialog to set/clear the alias for a chat.
func (cl *ChatList) showRenameDialog(chat models.Chat) {
	wins := fyne.CurrentApp().Driver().AllWindows()
	if len(wins) == 0 {
		return
	}
	entry := widget.NewEntry()
	entry.SetText(chatDisplayName(chat))

	d := dialog.NewForm("Rename contact", "Save", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Name", entry)},
		func(ok bool) {
			if !ok {
				return
			}
			alias := strings.TrimSpace(entry.Text)
			if alias == "" || alias == chat.GetDisplayName() {
				globalAliases.delete(chat.GUID)
			} else {
				globalAliases.set(chat.GUID, alias)
			}
			cl.rebuildVBox()
			if cl.onRename != nil {
				cl.onRename(chat.GUID)
			}
		}, wins[0])
	d.Show()
}

// ── chatItemWidget ────────────────────────────────────────────────────────────

// chatItemWidget is a single tappable chat row with right-click rename support.
type chatItemWidget struct {
	widget.BaseWidget
	chat     models.Chat // snapshot at creation time; GUID used for lookups
	chatList *ChatList

	dot  *widget.Label
	name *widget.Label
	host *fyne.Container
}

func newChatItemWidget(chat models.Chat, cl *ChatList, selected bool) *chatItemWidget {
	item := &chatItemWidget{chat: chat, chatList: cl}

	item.dot = widget.NewLabel("●")
	item.dot.Importance = widget.DangerImportance
	if !chat.HasNewMessage && chat.UnreadCount == 0 {
		item.dot.Hide()
	}

	item.name = widget.NewLabel(truncateString(chatDisplayName(chat), 28))
	item.name.Truncation = fyne.TextTruncateEllipsis
	if selected {
		item.name.TextStyle = fyne.TextStyle{Bold: true}
		item.name.Importance = widget.HighImportance
	}

	item.host = container.NewBorder(nil, nil, item.dot, nil, item.name)
	item.ExtendBaseWidget(item)
	return item
}

func (item *chatItemWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(item.host)
}

// Tapped handles primary click — select this chat.
func (item *chatItemWidget) Tapped(_ *fyne.PointEvent) {
	cl := item.chatList
	cl.selectedGUID = item.chat.GUID
	cl.rebuildVBox()
	for i := range cl.chats {
		if cl.chats[i].GUID == item.chat.GUID {
			chat := cl.chats[i]
			if cl.onSelect != nil {
				cl.onSelect(&chat)
			}
			return
		}
	}
}

// TappedSecondary handles right-click — show rename / clear-alias menu.
func (item *chatItemWidget) TappedSecondary(e *fyne.PointEvent) {
	cl := item.chatList
	guid := item.chat.GUID

	menuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Rename…", func() {
			for _, c := range cl.chats {
				if c.GUID == guid {
					cl.showRenameDialog(c)
					return
				}
			}
		}),
	}
	if globalAliases.get(guid) != "" {
		menuItems = append(menuItems, fyne.NewMenuItem("Clear alias", func() {
			globalAliases.delete(guid)
			cl.rebuildVBox()
			if cl.onRename != nil {
				cl.onRename(guid)
			}
		}))
	}

	c := fyne.CurrentApp().Driver().CanvasForObject(item)
	if c != nil {
		widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", menuItems...), c, e.AbsolutePosition)
	}
}
