package gui

import (
	"fmt"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

// MessageView renders the message history for the selected chat.
// All methods must be called from the Fyne main goroutine.
type MessageView struct {
	header   *widget.Label
	vbox     *fyne.Container
	scroll   *container.Scroll
	panel    fyne.CanvasObject
	messages []models.Message
}

func NewMessageView() *MessageView {
	mv := &MessageView{}
	mv.header = widget.NewLabel("")
	mv.header.TextStyle = fyne.TextStyle{Bold: true}

	mv.vbox = container.NewVBox()
	mv.scroll = container.NewVScroll(mv.vbox)
	mv.panel = container.NewBorder(mv.header, nil, nil, nil, mv.scroll)
	return mv
}

// Widget returns the full message panel (header + scroll area).
func (mv *MessageView) Widget() fyne.CanvasObject {
	return mv.panel
}

// SetChatName updates the chat name header.
func (mv *MessageView) SetChatName(name string) {
	mv.header.SetText(stripEmojis(name))
}

// SetMessages replaces all messages and scrolls to the bottom.
func (mv *MessageView) SetMessages(msgs []models.Message) {
	mv.messages = msgs
	mv.rebuildVBox()
}

// AppendMessage adds a single message, deduplicating by GUID.
// Ported from tui/messages.go AppendMessage.
func (mv *MessageView) AppendMessage(msg models.Message) {
	for _, existing := range mv.messages {
		if existing.GUID == msg.GUID {
			return
		}
	}
	mv.messages = append(mv.messages, msg)
	sort.Slice(mv.messages, func(i, j int) bool {
		return mv.messages[i].DateCreated < mv.messages[j].DateCreated
	})
	mv.rebuildVBox()
}

// SetFocused highlights the header when this pane is the focused one.
func (mv *MessageView) SetFocused(focused bool) {
	if focused {
		mv.header.Importance = widget.HighImportance
	} else {
		mv.header.Importance = widget.MediumImportance
	}
	mv.header.Refresh()
}

// ScrollToBottom scrolls the message list to the bottom after a short layout
// settle delay. Safe to call from the Fyne main goroutine.
func (mv *MessageView) ScrollToBottom() {
	go func() {
		time.Sleep(150 * time.Millisecond)
		fyne.Do(func() { mv.scroll.ScrollToBottom() })
	}()
}

func (mv *MessageView) rebuildVBox() {
	mv.vbox.Objects = nil
	for _, msg := range mv.messages {
		mv.vbox.Add(buildMessageRow(msg))
	}
	mv.vbox.Refresh()
	mv.ScrollToBottom()
}

func buildMessageRow(msg models.Message) fyne.CanvasObject {
	timeStr := formatMessageTime(msg.ParsedTime())

	var senderName string
	if msg.IsFromMe {
		senderName = "You"
	} else if msg.Handle != nil && msg.Handle.DisplayName != "" {
		senderName = stripEmojis(msg.Handle.DisplayName)
	} else if msg.Handle != nil {
		senderName = msg.Handle.Address
	} else {
		senderName = "Unknown"
	}

	text := fmt.Sprintf("[%s] %s: %s", timeStr, senderName, msg.Text)

	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord

	if msg.IsFromMe {
		label.Alignment = fyne.TextAlignTrailing
		label.Importance = widget.SuccessImportance
	}

	return label
}
