package gui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

// focusEntry is a single-line widget.Entry that fires onFocused / onBlurred
// on keyboard focus changes and intercepts shortcuts before the entry does.
type focusEntry struct {
	widget.Entry
	onFocused  func()
	onBlurred  func()
	onShortcut func(fyne.Shortcut) bool
	onSubmit   func(string)
	focused    bool
}

func newFocusEntry(placeholder string, onFocused func(), onShortcut func(fyne.Shortcut) bool, onSubmit func(string)) *focusEntry {
	e := &focusEntry{onFocused: onFocused, onShortcut: onShortcut, onSubmit: onSubmit}
	e.Entry.PlaceHolder = placeholder
	e.Entry.MultiLine = false
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
	if e.onBlurred != nil {
		e.onBlurred()
	}
}

func (e *focusEntry) IsFocused() bool { return e.focused }

// CreateRenderer wraps the standard Entry renderer and removes the
// focus-highlight border so only the blinking cursor is visible.
func (e *focusEntry) CreateRenderer() fyne.WidgetRenderer {
	return &focusEntryRenderer{inner: e.Entry.CreateRenderer()}
}

type focusEntryRenderer struct {
	inner fyne.WidgetRenderer
}

func (r *focusEntryRenderer) Layout(size fyne.Size)        { r.inner.Layout(size) }
func (r *focusEntryRenderer) MinSize() fyne.Size           { return r.inner.MinSize() }
func (r *focusEntryRenderer) Destroy()                     { r.inner.Destroy() }
func (r *focusEntryRenderer) Objects() []fyne.CanvasObject { return r.inner.Objects() }

func (r *focusEntryRenderer) Refresh() {
	r.inner.Refresh()
	for _, obj := range r.inner.Objects() {
		clearStrokeRecursive(obj)
		clearFillRecursive(obj)
	}
}

func clearStrokeRecursive(obj fyne.CanvasObject) {
	if obj == nil {
		return
	}
	if rect, ok := obj.(*canvas.Rectangle); ok {
		if rect.StrokeWidth != 0 || rect.StrokeColor != color.Transparent {
			rect.StrokeWidth = 0
			rect.StrokeColor = color.Transparent
			rect.Refresh()
		}
	}
	if containerObj, ok := obj.(*fyne.Container); ok {
		for _, child := range containerObj.Objects {
			clearStrokeRecursive(child)
		}
	}
}

func clearFillRecursive(obj fyne.CanvasObject) {
	if obj == nil {
		return
	}
	if rect, ok := obj.(*canvas.Rectangle); ok {
		if rect.FillColor != color.Transparent {
			rect.FillColor = color.Transparent
			rect.Refresh()
		}
	}
	if containerObj, ok := obj.(*fyne.Container); ok {
		for _, child := range containerObj.Objects {
			clearFillRecursive(child)
		}
	}
}

func (e *focusEntry) TypedShortcut(shortcut fyne.Shortcut) {
	if e.onShortcut != nil && e.onShortcut(shortcut) {
		return
	}
	e.Entry.TypedShortcut(shortcut)
}

func (e *focusEntry) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyEscape {
		e.Entry.FocusLost()
		e.focused = false
		return
	}
	if key.Name == fyne.KeyReturn || key.Name == fyne.KeyEnter {
		if e.onSubmit != nil {
			e.onSubmit(e.Text)
		}
		return
	}
	e.Entry.TypedKey(key)
}

// InputArea holds the single-line message entry and reply banner.
// All methods must be called from the Fyne main goroutine.
type InputArea struct {
	entry       *focusEntry
	panel       fyne.CanvasObject
	replyHolder *fyne.Container
	replyLabel  *canvas.Text
	cancelBtn   *glyphAction
	onSend      func(string, *models.Message)
	replyTarget *models.Message
}

func NewInputArea(onSend func(string, *models.Message), onFocused func()) *InputArea {
	return newInputArea(onSend, onFocused, nil)
}

func NewInputAreaWithShortcutHandler(onSend func(string, *models.Message), onFocused func(), onShortcut func(fyne.Shortcut) bool) *InputArea {
	return newInputArea(onSend, onFocused, onShortcut)
}

func newInputArea(onSend func(string, *models.Message), onFocused func(), onShortcut func(fyne.Shortcut) bool) *InputArea {
	ia := &InputArea{onSend: onSend}

	ia.entry = newFocusEntry("", onFocused, onShortcut, func(text string) {
		ia.submit(text)
	})
	ia.replyLabel = canvas.NewText("", theme.Color(theme.ColorNameDisabled))
	ia.replyHolder = container.NewMax()
	ia.cancelBtn = newGlyphAction("×", func() { ia.ClearReplyTarget() })

	indent := inputSideIndent()
	indentedEntry := container.NewBorder(nil, nil, fixedWidthSpacer(indent), fixedWidthSpacer(indent), ia.entry)
	ia.panel = container.NewVBox(ia.replyHolder, indentedEntry)
	ia.RefreshLayout()
	return ia
}

// Widget returns the input panel for embedding in layouts.
func (ia *InputArea) Widget() fyne.CanvasObject { return ia.panel }

// IsEntryFocused reports whether the text entry currently has keyboard focus.
func (ia *InputArea) IsEntryFocused() bool { return ia.entry.IsFocused() }

// SetOnBlurred registers a callback fired when the text entry loses keyboard focus.
func (ia *InputArea) SetOnBlurred(fn func()) { ia.entry.onBlurred = fn }

// FocusEntry requests keyboard focus for the input entry.
func (ia *InputArea) FocusEntry(c fyne.Canvas) {
	if c != nil {
		c.Focus(ia.entry)
	}
}

// SetReplyTarget enables reply mode for the next sent message.
func (ia *InputArea) SetReplyTarget(msg models.Message) {
	reply := msg
	ia.replyTarget = &reply
	ia.replyLabel.Text = "Replying to: " + truncateString(stripEmojis(msg.Text), 80)
	ia.replyLabel.Refresh()
	ia.replyHolder.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, nil, ia.cancelBtn, ia.replyLabel)}
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
	ia.ClearReplyTarget()
	if ia.onSend != nil {
		ia.onSend(text, reply)
	}
}

func (ia *InputArea) RefreshLayout() {
	if ia.replyLabel != nil {
		ia.replyLabel.TextSize = hoverTimestampTextSize()
		ia.replyLabel.Color = theme.Color(theme.ColorNameDisabled)
		ia.replyLabel.Refresh()
	}
	if ia.cancelBtn != nil {
		ia.cancelBtn.SetTextSize(glyphTextSize())
	}
}

// Ensure focusEntry satisfies desktop.Mouseable so Fyne doesn't lose hover
// state when the cursor is over the entry inside the floating card.
func (e *focusEntry) MouseIn(_ *desktop.MouseEvent)    {}
func (e *focusEntry) MouseOut()                        {}
func (e *focusEntry) MouseMoved(_ *desktop.MouseEvent) {}
