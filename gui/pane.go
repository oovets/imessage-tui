package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"github.com/bluebubbles-tui/models"
)

var paneIDCounter int

// ChatPane is a single panel showing one conversation (messages + input).
type ChatPane struct {
	id        int
	msgView   *MessageView
	inputArea *InputArea
	ChatGUID  string
	widget    fyne.CanvasObject
}

func newChatPane(onSend func(*ChatPane, string, *models.Message), onFocused func(*ChatPane), onInputShortcut func(fyne.Shortcut) bool) *ChatPane {
	p := &ChatPane{id: paneIDCounter}
	paneIDCounter++

	p.msgView = NewMessageView(func(msg models.Message) {
		onFocused(p)
		p.inputArea.SetReplyTarget(msg)
	})
	p.inputArea = NewInputAreaWithShortcutHandler(
		func(text string, replyTo *models.Message) { onSend(p, text, replyTo) },
		func() { onFocused(p) },
		onInputShortcut,
	)
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(1, 12))
	gapBelow := canvas.NewRectangle(color.Transparent)
	gapBelow.SetMinSize(fyne.NewSize(1, 12))
	inputWithGap := container.NewVBox(gap, p.inputArea.Widget(), gapBelow)
	p.widget = container.NewBorder(nil, inputWithGap, nil, nil, p.msgView.Widget())
	return p
}

// Widget returns the full pane canvas object.
func (p *ChatPane) Widget() fyne.CanvasObject { return p.widget }

// SetFocused toggles the visual focus indicator on the message header.
func (p *ChatPane) SetFocused(focused bool) {
	p.msgView.SetFocused(focused)
}

// IsInputFocused reports whether this pane's message entry has keyboard focus.
func (p *ChatPane) IsInputFocused() bool {
	return p.inputArea.IsEntryFocused()
}

// ClearReplyTarget exits reply mode for this pane.
func (p *ChatPane) ClearReplyTarget() {
	p.inputArea.ClearReplyTarget()
}

// FocusInput requests keyboard focus for this pane's message entry.
func (p *ChatPane) FocusInput(c fyne.Canvas) {
	p.inputArea.FocusEntry(c)
}
