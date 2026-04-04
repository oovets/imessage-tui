package gui

import (
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oovets/bluebubbles-gui/models"
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

	rebuildMu    sync.Mutex
	rebuildTimer *time.Timer
}

// SetSelected marks a chat as active and rebuilds the list to show the indicator.
func (cl *ChatList) SetSelected(guid string) {
	if cl.selectedGUID == guid {
		return
	}
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

// ApplyMessageActivity updates preview/unread state and moves the chat to the
// top of its section when new activity arrives.
func (cl *ChatList) ApplyMessageActivity(chatGUID string, msg models.Message, markUnread bool) {
	for i, chat := range cl.chats {
		if chat.GUID != chatGUID {
			continue
		}
		changed := false
		preview := messagePreviewText(msg)
		if cl.chats[i].LastMessageText != preview {
			cl.chats[i].LastMessageText = preview
			changed = true
		}
		if markUnread {
			if !cl.chats[i].HasNewMessage {
				cl.chats[i].HasNewMessage = true
				changed = true
			}
		}
		if cl.moveChatToSectionTop(i) {
			changed = true
		}
		if changed {
			cl.queueRebuild()
		}
		return
	}
}

// MarkNewMessage moves the chat to the top of its section and shows the new-message dot.
func (cl *ChatList) MarkNewMessage(chatGUID string) {
	for i, chat := range cl.chats {
		if chat.GUID != chatGUID {
			continue
		}
		changed := false
		if !cl.chats[i].HasNewMessage {
			cl.chats[i].HasNewMessage = true
			changed = true
		}
		if cl.moveChatToSectionTop(i) {
			changed = true
		}
		if changed {
			cl.queueRebuild()
		}
		return
	}
}

func (cl *ChatList) moveChatToSectionTop(index int) bool {
	if index < 0 || index >= len(cl.chats) {
		return false
	}
	isGroup := cl.chats[index].IsGroup()
	sectionStart := 0
	for j, c := range cl.chats {
		if c.IsGroup() == isGroup {
			sectionStart = j
			break
		}
	}
	if index <= sectionStart {
		return false
	}
	moving := cl.chats[index]
	copy(cl.chats[sectionStart+1:index+1], cl.chats[sectionStart:index])
	cl.chats[sectionStart] = moving
	return true
}

// ClearNewMessage clears the new-message indicator.
func (cl *ChatList) ClearNewMessage(chatGUID string) {
	for i, chat := range cl.chats {
		if chat.GUID == chatGUID {
			if !cl.chats[i].HasNewMessage {
				return
			}
			cl.chats[i].HasNewMessage = false
			cl.rebuildVBox()
			return
		}
	}
}

func (cl *ChatList) queueRebuild() {
	cl.rebuildMu.Lock()
	if cl.rebuildTimer != nil {
		cl.rebuildMu.Unlock()
		return
	}
	cl.rebuildTimer = time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(func() {
			cl.rebuildVBox()
		})
		cl.rebuildMu.Lock()
		cl.rebuildTimer = nil
		cl.rebuildMu.Unlock()
	})
	cl.rebuildMu.Unlock()
}

// rebuildVBox rebuilds the VBox content from cl.chats.
func (cl *ChatList) rebuildVBox() {
	defer perfStart("ChatList.rebuildVBox")()
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
	l := widget.NewLabel(strings.ToUpper(title))
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.Importance = widget.LowImportance
	return container.NewPadded(l)
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

	badge   *widget.Label
	badgeBg *canvas.Rectangle
	bg      *canvas.Rectangle
	accent  *canvas.Rectangle
	name    *widget.Label
	preview *widget.Label
	nameCol *fyne.Container
	host    *fyne.Container
}

func newChatItemWidget(chat models.Chat, cl *ChatList, selected bool) *chatItemWidget {
	item := &chatItemWidget{chat: chat, chatList: cl}
	hasUnread := chat.HasNewMessage || chat.UnreadCount > 0
	rowBg, accentColor, badgeBg := chatItemColors(selected, hasUnread)

	item.badge = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	item.badgeBg = canvas.NewRectangle(badgeBg)
	item.badgeBg.CornerRadius = 8
	if chat.UnreadCount > 0 {
		item.badge.SetText(strconv.Itoa(chat.UnreadCount))
	} else if chat.HasNewMessage {
		item.badge.SetText("●")
	}
	if item.badge.Text == "" {
		item.badge.Hide()
		item.badgeBg.Hide()
	}
	item.badge.Importance = widget.HighImportance
	badgeObj := container.NewStack(item.badgeBg, container.NewPadded(item.badge))
	if item.badge.Text == "" {
		badgeObj.Hide()
	}

	item.name = widget.NewLabel(truncateString(chatDisplayName(chat), 28))
	item.name.Truncation = fyne.TextTruncateEllipsis
	item.preview = widget.NewLabel(truncateString(chatPreviewText(chat), 36))
	item.preview.Truncation = fyne.TextTruncateEllipsis
	item.preview.Importance = widget.MediumImportance
	item.preview.TextStyle = fyne.TextStyle{Italic: !selected}
	item.nameCol = container.NewVBox(item.name, item.preview)
	if selected {
		item.name.TextStyle = fyne.TextStyle{Bold: true}
		item.name.Importance = widget.HighImportance
		item.preview.Importance = widget.HighImportance
	}
	item.bg = canvas.NewRectangle(rowBg)
	item.bg.CornerRadius = 0
	item.accent = canvas.NewRectangle(accentColor)
	item.accent.SetMinSize(fyne.NewSize(3, 1))
	accentSpacer := container.NewVBox(item.accent)
	rowContent := container.NewBorder(nil, nil, accentSpacer, badgeObj, item.nameCol)
	if !selected && !(chat.HasNewMessage || chat.UnreadCount > 0) {
		item.accent.Hide()
	}

	item.host = container.NewPadded(container.NewStack(item.bg, container.NewPadded(rowContent)))
	item.ExtendBaseWidget(item)
	return item
}

func chatItemColors(selected bool, unread bool) (color.Color, color.Color, color.Color) {
	primary := colorToNRGBA(theme.Color(theme.ColorNamePrimary))
	inputBg := colorToNRGBA(theme.Color(theme.ColorNameInputBackground))
	bg := colorToNRGBA(theme.Color(theme.ColorNameBackground))

	base := color.NRGBA{R: inputBg.R, G: inputBg.G, B: inputBg.B, A: 0}
	if unread {
		base = color.NRGBA{R: primary.R, G: primary.G, B: primary.B, A: 52}
	}
	if selected {
		base = color.NRGBA{R: primary.R, G: primary.G, B: primary.B, A: 46}
	}
	accent := color.NRGBA{R: primary.R, G: primary.G, B: primary.B, A: 220}
	badgeBg := color.NRGBA{R: primary.R, G: primary.G, B: primary.B, A: 220}
	if !selected && unread {
		badgeBg = color.NRGBA{R: primary.R, G: primary.G, B: primary.B, A: 185}
	}
	if !selected && !unread {
		accent = color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 0}
	}
	return base, accent, badgeBg
}

func chatPreviewText(chat models.Chat) string {
	if text := strings.TrimSpace(chat.LastMessageText); text != "" {
		return stripEmojis(text)
	}
	if chat.LastMessage != nil {
		text := strings.TrimSpace(chat.LastMessage.Text)
		if text == "" {
			if len(chat.LastMessage.Attachments) > 0 {
				return "Attachment"
			}
			return "No messages"
		}
		if chat.LastMessage.IsFromMe {
			return "You: " + stripEmojis(text)
		}
		return stripEmojis(text)
	}
	return "No messages"
}

func messagePreviewText(msg models.Message) string {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		if len(msg.Attachments) > 0 {
			text = "Attachment"
		} else {
			text = "No messages"
		}
	}
	text = stripEmojis(text)
	if msg.IsFromMe && text != "No messages" && text != "Attachment" {
		return "You: " + text
	}
	return text
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
