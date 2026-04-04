package gui

import (
	"image/color"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oovets/bluebubbles-gui/models"
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
	e.Entry.MultiLine = true
	e.Entry.Wrapping = fyne.TextWrapWord
	e.Entry.SetMinRowsVisible(1)
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

// InputArea holds the multiline message entry and reply banner.
// All methods must be called from the Fyne main goroutine.
type InputArea struct {
	entry           *focusEntry
	panel           fyne.CanvasObject
	replyHolder     *fyne.Container
	replyLabel      *canvas.Text
	cancelBtn       *glyphAction
	sendBtn         *glyphAction
	statusLabel     *widget.Label
	inputBg         *canvas.Rectangle
	inputBorder     *canvas.Rectangle
	inputShadow     *canvas.Rectangle
	entryRows       int
	onSend          func(string, *models.Message)
	replyTarget     *models.Message
	onLayoutChanged func()
	statusSeq       atomic.Uint64
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
	ia.entry.OnChanged = func(text string) {
		if ia.adjustEntryRows(text) {
			ia.notifyLayoutChanged()
		}
	}
	ia.entry.SetMinRowsVisible(1)
	ia.replyLabel = canvas.NewText("", theme.Color(theme.ColorNameDisabled))
	ia.replyHolder = container.NewMax()
	ia.cancelBtn = newGlyphAction("×", func() { ia.ClearReplyTarget() })
	ia.sendBtn = nil
	ia.statusLabel = widget.NewLabel("")
	ia.statusLabel.Hide()

	inputHPad := float32(10)
	ia.inputShadow = canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 24})
	ia.inputBorder = canvas.NewRectangle(theme.Color(theme.ColorNameInputBorder))
	ia.inputBg = canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	entryRow := container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		ia.entry,
	)
	cardContent := container.NewPadded(container.NewVBox(
		ia.replyHolder,
		container.NewBorder(nil, nil, fixedWidthSpacer(inputHPad), fixedWidthSpacer(inputHPad), entryRow),
		ia.statusLabel,
	))
	ia.replyHolder.Hide()
	rawPanel := container.NewPadded(container.NewStack(
		container.NewPadded(ia.inputShadow),
		ia.inputBorder,
		ia.inputBg,
		cardContent,
	))
	ia.panel = rawPanel
	ia.adjustEntryRows("")
	ia.RefreshLayout()
	return ia
}

// Widget returns the input panel for embedding in layouts.
func (ia *InputArea) Widget() fyne.CanvasObject { return ia.panel }

// IsEntryFocused reports whether the text entry currently has keyboard focus.
func (ia *InputArea) IsEntryFocused() bool { return ia.entry.IsFocused() }

// SetOnBlurred registers a callback fired when the text entry loses keyboard focus.
func (ia *InputArea) SetOnBlurred(fn func()) { ia.entry.onBlurred = fn }

func (ia *InputArea) SetOnLayoutChanged(fn func()) { ia.onLayoutChanged = fn }

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
	preview := truncateString(stripEmojis(msg.Text), 72)
	if preview == "" {
		preview = "Attachment"
	}
	ia.replyLabel.Text = "Replying to " + messageSenderName(msg) + ": " + preview
	ia.replyLabel.Refresh()
	chipBg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	chip := container.NewStack(
		chipBg,
		container.NewPadded(container.NewBorder(nil, nil, nil, ia.cancelBtn, ia.replyLabel)),
	)
	ia.replyHolder.Objects = []fyne.CanvasObject{chip}
	ia.replyHolder.Show()
	ia.replyHolder.Refresh()
	ia.notifyLayoutChanged()
}

// ClearReplyTarget exits reply mode.
func (ia *InputArea) ClearReplyTarget() {
	ia.replyTarget = nil
	ia.replyHolder.Objects = nil
	ia.replyHolder.Hide()
	ia.replyHolder.Refresh()
	ia.notifyLayoutChanged()
}

func (ia *InputArea) submit(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	reply := ia.replyTarget
	ia.entry.SetText("")
	ia.adjustEntryRows("")
	ia.ClearReplyTarget()
	if ia.onSend != nil {
		ia.onSend(text, reply)
	}
}

func (ia *InputArea) adjustEntryRows(text string) bool {
	if ia == nil || ia.entry == nil {
		return false
	}

	entryWidth := ia.entry.Size().Width
	if entryWidth <= 0 {
		entryWidth = 320
	}

	charWidth := fyne.MeasureText("M", float32(theme.TextSize()), fyne.TextStyle{}).Width
	if charWidth <= 0 {
		charWidth = 8
	}
	cols := int(entryWidth / charWidth)
	if cols < 8 {
		cols = 8
	}

	rows := 1
	if strings.TrimSpace(text) != "" {
		rows = 0
		for _, line := range strings.Split(text, "\n") {
			r := len([]rune(line))
			if r == 0 {
				rows++
				continue
			}
			rows += (r + cols - 1) / cols
		}
	}

	if rows < 1 {
		rows = 1
	}
	if rows > 6 {
		rows = 6
	}
	if rows == ia.entryRows {
		return false
	}
	ia.entryRows = rows
	ia.entry.SetMinRowsVisible(rows)
	return true
}

func (ia *InputArea) SetTransientStatus(text string) {
	seq := ia.statusSeq.Add(1)
	ia.statusLabel.SetText(text)
	ia.statusLabel.Show()
	ia.statusLabel.Refresh()
	ia.notifyLayoutChanged()
	time.AfterFunc(1800*time.Millisecond, func() {
		fyne.Do(func() {
			if ia.statusSeq.Load() != seq {
				return
			}
			ia.statusLabel.Hide()
			ia.statusLabel.SetText("")
			ia.statusLabel.Refresh()
			ia.notifyLayoutChanged()
		})
	})
}

func (ia *InputArea) RefreshLayout() {
	baseBorder := colorToNRGBA(theme.Color(theme.ColorNameInputBorder))
	if ia.inputBg != nil {
		ia.inputBg.FillColor = color.Transparent
		ia.inputBg.StrokeWidth = 0
		ia.inputBg.StrokeColor = color.Transparent
		ia.inputBg.Refresh()
	}
	if ia.inputBorder != nil {
		ia.inputBorder.FillColor = color.Transparent
		ia.inputBorder.StrokeWidth = 0.6
		ia.inputBorder.StrokeColor = color.NRGBA{R: baseBorder.R, G: baseBorder.G, B: baseBorder.B, A: 90}
		ia.inputBorder.Refresh()
	}
	if ia.inputShadow != nil {
		ia.inputShadow.FillColor = color.Transparent
		ia.inputShadow.StrokeWidth = 0
		ia.inputShadow.StrokeColor = color.Transparent
		ia.inputShadow.Refresh()
	}
	if ia.replyLabel != nil {
		ia.replyLabel.TextSize = hoverTimestampTextSize()
		ia.replyLabel.Color = theme.Color(theme.ColorNameForeground)
		ia.replyLabel.Refresh()
	}
	if ia.cancelBtn != nil {
		ia.cancelBtn.SetTextSize(glyphTextSize())
		ia.cancelBtn.SetEmphasis(true)
	}
	if ia.sendBtn != nil {
		ia.sendBtn.SetTextSize(glyphTextSize() + 1)
		ia.sendBtn.SetFixedColor(theme.Color(theme.ColorNamePrimary))
	}
	if ia.statusLabel != nil {
		ia.statusLabel.Importance = widget.MediumImportance
	}
	ia.notifyLayoutChanged()
	if ia.entry != nil {
		ia.entry.Refresh()
	}
}

func (ia *InputArea) notifyLayoutChanged() {
	if ia.onLayoutChanged != nil {
		ia.onLayoutChanged()
	}
}

// Ensure focusEntry satisfies desktop.Mouseable so Fyne doesn't lose hover
// state when the cursor is over the entry inside the floating card.
func (e *focusEntry) MouseIn(_ *desktop.MouseEvent)    {}
func (e *focusEntry) MouseOut()                        {}
func (e *focusEntry) MouseMoved(_ *desktop.MouseEvent) {}
