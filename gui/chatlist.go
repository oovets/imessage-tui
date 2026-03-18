package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

// ChatList renders the left-side conversation list.
// All methods must be called from the Fyne main goroutine.
type ChatList struct {
	list     *widget.List
	chats    []models.Chat
	onSelect func(*models.Chat)
}

func NewChatList(onSelect func(*models.Chat)) *ChatList {
	cl := &ChatList{onSelect: onSelect}

	cl.list = widget.NewList(
		func() int { return len(cl.chats) },
		func() fyne.CanvasObject {
			dot := widget.NewLabel("●")
			dot.Importance = widget.DangerImportance
			name := widget.NewLabel("")
			name.Truncation = fyne.TextTruncateEllipsis
			// Border layout: dot pinned left, name fills remaining width
			return container.NewBorder(nil, nil, dot, nil, name)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if int(id) >= len(cl.chats) {
				return
			}
			chat := cl.chats[id]
			row := obj.(*fyne.Container)
			// Border layout: Objects[0] is the center (name), Objects[1] is left (dot)
			name := row.Objects[0].(*widget.Label)
			dot := row.Objects[1].(*widget.Label)

			if chat.HasNewMessage || chat.UnreadCount > 0 {
				dot.Show()
			} else {
				dot.Hide()
			}
			name.SetText(truncateString(stripEmojis(chat.GetDisplayName()), 28))
		},
	)

	cl.list.OnSelected = func(id widget.ListItemID) {
		if int(id) >= len(cl.chats) {
			return
		}
		chat := cl.chats[id]
		if cl.onSelect != nil {
			cl.onSelect(&chat)
		}
	}

	return cl
}

// Widget returns the Fyne canvas object for embedding in layouts.
func (cl *ChatList) Widget() fyne.CanvasObject {
	return cl.list
}

// SetChats replaces the chat list (call from Fyne main goroutine).
func (cl *ChatList) SetChats(chats []models.Chat) {
	cl.chats = make([]models.Chat, len(chats))
	copy(cl.chats, chats)
	cl.list.Refresh()
}

// MarkNewMessage moves the given chat to the top and sets its new-message flag.
func (cl *ChatList) MarkNewMessage(chatGUID string) {
	for i, chat := range cl.chats {
		if chat.GUID == chatGUID {
			cl.chats[i].HasNewMessage = true
			if i > 0 {
				moving := cl.chats[i]
				copy(cl.chats[1:i+1], cl.chats[0:i])
				cl.chats[0] = moving
			}
			cl.list.Refresh()
			return
		}
	}
}

// ClearNewMessage clears the new-message indicator for the given chat.
func (cl *ChatList) ClearNewMessage(chatGUID string) {
	for i, chat := range cl.chats {
		if chat.GUID == chatGUID {
			cl.chats[i].HasNewMessage = false
			cl.list.Refresh()
			return
		}
	}
}
