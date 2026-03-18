package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

// focusEntry is a widget.Entry that fires onFocused when it gains keyboard focus.
// This lets the PaneManager know which pane the user is interacting with.
type focusEntry struct {
	widget.Entry
	onFocused  func()
	onShortcut func(fyne.Shortcut) bool
	focused    bool
}

func newFocusEntry(placeholder string, onFocused func(), onShortcut func(fyne.Shortcut) bool) *focusEntry {
	e := &focusEntry{onFocused: onFocused, onShortcut: onShortcut}
	e.Entry.PlaceHolder = placeholder
	e.ExtendBaseWidget(e)
	return e
}

func (e *focusEntry) FocusGained() {
	e.Entry.FocusGained()
	e.focused = true
	if e.onFocused != nil {
		e.onFocused()
	}
}

func (e *focusEntry) FocusLost() {
	e.Entry.FocusLost()
	e.focused = false
}

func (e *focusEntry) IsFocused() bool {
	return e.focused
}

func (e *focusEntry) TypedShortcut(shortcut fyne.Shortcut) {
	if e.onShortcut != nil && e.onShortcut(shortcut) {
		return
	}
	e.Entry.TypedShortcut(shortcut)
}

// InputArea holds the message entry box and send button.
// All methods must be called from the Fyne main goroutine.
type InputArea struct {
	entry       *focusEntry
	sendBtn     *widget.Button
	panel       fyne.CanvasObject
	replyHolder *fyne.Container
	replyLabel  *widget.Label
	onSend      func(string, *models.Message)
	replyTarget *models.Message
}

func NewInputArea(onSend func(string, *models.Message), onFocused func()) *InputArea {
	ia := &InputArea{onSend: onSend}

	ia.entry = newFocusEntry("Type a message…", onFocused, nil)
	ia.entry.OnSubmitted = func(text string) {
		ia.submit(text)
	}
	ia.replyLabel = widget.NewLabel("")
	ia.replyLabel.Wrapping = fyne.TextWrapWord
	ia.replyLabel.Importance = widget.LowImportance
	ia.replyHolder = container.NewMax()

	ia.sendBtn = widget.NewButton("Send", func() {
		ia.submit(ia.entry.Text)
	})

	inputRow := container.NewBorder(nil, nil, nil, ia.sendBtn, ia.entry)
	ia.panel = container.NewVBox(ia.replyHolder, inputRow)
	return ia
}

func NewInputAreaWithShortcutHandler(onSend func(string, *models.Message), onFocused func(), onShortcut func(fyne.Shortcut) bool) *InputArea {
	ia := &InputArea{onSend: onSend}

	ia.entry = newFocusEntry("Type a message…", onFocused, onShortcut)
	ia.entry.OnSubmitted = func(text string) {
		ia.submit(text)
	}
	ia.replyLabel = widget.NewLabel("")
	ia.replyLabel.Wrapping = fyne.TextWrapWord
	ia.replyLabel.Importance = widget.LowImportance
	ia.replyHolder = container.NewMax()

	ia.sendBtn = widget.NewButton("Send", func() {
		ia.submit(ia.entry.Text)
	})

	inputRow := container.NewBorder(nil, nil, nil, ia.sendBtn, ia.entry)
	ia.panel = container.NewVBox(ia.replyHolder, inputRow)
	return ia
}

// Widget returns the input panel for embedding in layouts.
func (ia *InputArea) Widget() fyne.CanvasObject {
	return ia.panel
}

// IsEntryFocused reports whether the text entry currently has keyboard focus.
func (ia *InputArea) IsEntryFocused() bool {
	return ia.entry.IsFocused()
}

// FocusEntry requests keyboard focus for the input entry.
func (ia *InputArea) FocusEntry(c fyne.Canvas) {
	if c == nil {
		return
	}
	c.Focus(ia.entry)
}

// SetReplyTarget enables reply mode for the next sent message.
func (ia *InputArea) SetReplyTarget(msg models.Message) {
	reply := msg
	ia.replyTarget = &reply
	ia.replyLabel.SetText("Replying to: " + truncateString(stripEmojis(msg.Text), 80))
	cancelBtn := newGlyphAction("×", func() {
		ia.ClearReplyTarget()
	})
	ia.replyHolder.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, nil, cancelBtn, ia.replyLabel)}
	ia.replyHolder.Refresh()
}

// ClearReplyTarget exits reply mode.
func (ia *InputArea) ClearReplyTarget() {
	ia.replyTarget = nil
	ia.replyHolder.Objects = nil
	ia.replyHolder.Refresh()
}

func (ia *InputArea) submit(text string) {
	if text == "" {
		return
	}
	reply := ia.replyTarget
	ia.entry.SetText("")
	ia.ClearReplyTarget()
	if ia.onSend != nil {
		ia.onSend(text, reply)
	}
}
