package gui

import (
	"image/color"
	"math"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

// focusEntry is a widget.Entry that fires onFocused when it gains keyboard focus.
// This lets the PaneManager know which pane the user is interacting with.
type focusEntry struct {
	widget.Entry
	onFocused  func()
	onShortcut func(fyne.Shortcut) bool
	onSubmit   func(string)
	focused    bool
	skipReturn bool
}

func newFocusEntry(placeholder string, onFocused func(), onShortcut func(fyne.Shortcut) bool, onSubmit func(string)) *focusEntry {
	e := &focusEntry{onFocused: onFocused, onShortcut: onShortcut, onSubmit: onSubmit}
	e.Entry.PlaceHolder = placeholder
	e.Entry.MultiLine = true
	e.Entry.Wrapping = fyne.TextWrapWord
	e.Entry.SetMinRowsVisible(2)
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

// CreateRenderer wraps the standard Entry renderer and removes the
// focus-highlight border so only the blinking cursor is visible.
func (e *focusEntry) CreateRenderer() fyne.WidgetRenderer {
	return &focusEntryRenderer{inner: e.Entry.CreateRenderer()}
}

type focusEntryRenderer struct {
	inner fyne.WidgetRenderer
}

func (r *focusEntryRenderer) Layout(size fyne.Size)            { r.inner.Layout(size) }
func (r *focusEntryRenderer) MinSize() fyne.Size               { return r.inner.MinSize() }
func (r *focusEntryRenderer) Destroy()                         { r.inner.Destroy() }
func (r *focusEntryRenderer) Objects() []fyne.CanvasObject     { return r.inner.Objects() }

func (r *focusEntryRenderer) Refresh() {
	r.inner.Refresh()
	// Fyne draws a primary-coloured stroke border when the entry is focused.
	// Clear any stroke on the top-level rectangles so only the cursor shows.
	for _, obj := range r.inner.Objects() {
		if rect, ok := obj.(*canvas.Rectangle); ok && rect.StrokeWidth > 0 {
			rect.StrokeWidth = 0
			rect.StrokeColor = color.Transparent
			rect.Refresh()
		}
	}
}

func (e *focusEntry) TypedShortcut(shortcut fyne.Shortcut) {
	if custom, ok := shortcut.(*desktop.CustomShortcut); ok {
		if custom.KeyName == fyne.KeyReturn || custom.KeyName == fyne.KeyEnter {
			if custom.Modifier&fyne.KeyModifierShift != 0 {
				e.skipReturn = true
				e.Entry.TypedRune('\n')
				return
			}
		}
	}
	if e.onShortcut != nil && e.onShortcut(shortcut) {
		return
	}
	e.Entry.TypedShortcut(shortcut)
}

func (e *focusEntry) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyReturn || key.Name == fyne.KeyEnter {
		if e.skipReturn {
			e.skipReturn = false
			return
		}
		if e.onSubmit != nil {
			e.onSubmit(e.Text)
		}
		return
	}
	e.Entry.TypedKey(key)
}

// InputArea holds the message entry box and send button.
// All methods must be called from the Fyne main goroutine.
type InputArea struct {
	entry       *focusEntry
	panel       fyne.CanvasObject
	replyHolder *fyne.Container
	replyLabel  *widget.Label
	onSend      func(string, *models.Message)
	replyTarget *models.Message
}

func NewInputArea(onSend func(string, *models.Message), onFocused func()) *InputArea {
	ia := &InputArea{onSend: onSend}

	ia.entry = newFocusEntry("Chat 4 Lyfe...", onFocused, nil, func(text string) {
		ia.submit(text)
	})
	ia.entry.OnChanged = func(_ string) { ia.updateInputHeight() }
	ia.replyLabel = widget.NewLabel("")
	ia.replyLabel.Wrapping = fyne.TextWrapWord
	ia.replyLabel.Importance = widget.LowImportance
	ia.replyHolder = container.NewMax()

	indentedEntry := container.NewBorder(nil, nil, fixedWidthSpacer(16), fixedWidthSpacer(16), ia.entry)
	ia.panel = container.NewVBox(ia.replyHolder, indentedEntry)
	ia.updateInputHeight()
	return ia
}

func NewInputAreaWithShortcutHandler(onSend func(string, *models.Message), onFocused func(), onShortcut func(fyne.Shortcut) bool) *InputArea {
	ia := &InputArea{onSend: onSend}

	ia.entry = newFocusEntry("Chat 4 Lyfe...", onFocused, onShortcut, func(text string) {
		ia.submit(text)
	})
	ia.entry.OnChanged = func(_ string) { ia.updateInputHeight() }
	ia.replyLabel = widget.NewLabel("")
	ia.replyLabel.Wrapping = fyne.TextWrapWord
	ia.replyLabel.Importance = widget.LowImportance
	ia.replyHolder = container.NewMax()

	indentedEntry := container.NewBorder(nil, nil, fixedWidthSpacer(16), fixedWidthSpacer(16), ia.entry)
	ia.panel = container.NewVBox(ia.replyHolder, indentedEntry)
	ia.updateInputHeight()
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
	if strings.TrimSpace(text) == "" {
		return
	}
	reply := ia.replyTarget
	ia.entry.SetText("")
	ia.updateInputHeight()
	ia.ClearReplyTarget()
	if ia.onSend != nil {
		ia.onSend(text, reply)
	}
}

func (ia *InputArea) updateInputHeight() {
	if ia.entry == nil {
		return
	}

	textSize := float32(theme.TextSize())
	lineHeight := fyne.MeasureText("Mg", textSize, fyne.TextStyle{}).Height
	if lineHeight <= 0 {
		lineHeight = 16
	}

	availableWidth := ia.entry.Size().Width
	if availableWidth <= 0 {
		availableWidth = 320
	}

	wrappedLines := estimateWrappedLines(ia.entry.Text, availableWidth, textSize)
	if wrappedLines < 2 {
		wrappedLines = 2
	}
	if wrappedLines > 8 {
		wrappedLines = 8
	}

	ia.entry.SetMinRowsVisible(wrappedLines)
	ia.entry.Refresh()
}

func estimateWrappedLines(text string, width float32, textSize float32) int {
	if strings.TrimSpace(text) == "" {
		return 1
	}
	lines := 0
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if line == "" {
			lines++
			continue
		}
		measure := fyne.MeasureText(line, textSize, fyne.TextStyle{}).Width
		if width <= 0 {
			width = 1
		}
		lines += int(math.Max(1, math.Ceil(float64(measure/width))))
	}
	if lines < 1 {
		return 1
	}
	return lines
}
