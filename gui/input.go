package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// focusEntry is a widget.Entry that fires onFocused when it gains keyboard focus.
// This lets the PaneManager know which pane the user is interacting with.
type focusEntry struct {
	widget.Entry
	onFocused func()
}

func newFocusEntry(placeholder string, onFocused func()) *focusEntry {
	e := &focusEntry{onFocused: onFocused}
	e.Entry.PlaceHolder = placeholder
	e.ExtendBaseWidget(e)
	return e
}

func (e *focusEntry) FocusGained() {
	e.Entry.FocusGained()
	if e.onFocused != nil {
		e.onFocused()
	}
}

// InputArea holds the message entry box and send button.
// All methods must be called from the Fyne main goroutine.
type InputArea struct {
	entry   *focusEntry
	sendBtn *widget.Button
	panel   fyne.CanvasObject
	onSend  func(string)
}

func NewInputArea(onSend func(string), onFocused func()) *InputArea {
	ia := &InputArea{onSend: onSend}

	ia.entry = newFocusEntry("Type a message…", onFocused)
	ia.entry.OnSubmitted = func(text string) {
		ia.submit(text)
	}

	ia.sendBtn = widget.NewButton("Send", func() {
		ia.submit(ia.entry.Text)
	})

	ia.panel = container.NewBorder(nil, nil, nil, ia.sendBtn, ia.entry)
	return ia
}

// Widget returns the input panel for embedding in layouts.
func (ia *InputArea) Widget() fyne.CanvasObject {
	return ia.panel
}

func (ia *InputArea) submit(text string) {
	if text == "" {
		return
	}
	ia.entry.SetText("")
	if ia.onSend != nil {
		ia.onSend(text)
	}
}
