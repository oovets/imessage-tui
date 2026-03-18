package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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

func newChatPane(onSend func(*ChatPane, string), onFocused func(*ChatPane)) *ChatPane {
	p := &ChatPane{id: paneIDCounter}
	paneIDCounter++

	p.msgView = NewMessageView()
	p.inputArea = NewInputArea(
		func(text string) { onSend(p, text) },
		func() { onFocused(p) },
	)
	p.widget = container.NewBorder(nil, p.inputArea.Widget(), nil, nil, p.msgView.Widget())
	return p
}

// Widget returns the full pane canvas object.
func (p *ChatPane) Widget() fyne.CanvasObject { return p.widget }

// SetFocused toggles the visual focus indicator on the message header.
func (p *ChatPane) SetFocused(focused bool) {
	p.msgView.SetFocused(focused)
}
