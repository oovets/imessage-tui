package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/models"
)

var paneIDCounter int

// ChatPane is a single panel showing one conversation (messages + input).
type ChatPane struct {
	id           int
	msgView      *MessageView
	inputArea    *InputArea
	ChatGUID     string
	widget       *fyne.Container
	surface      *paneSurface
	inputVisible bool
}

type paneSurface struct {
	widget.BaseWidget
	content    fyne.CanvasObject
	onActivate func()
}

func newPaneSurface(content fyne.CanvasObject, onActivate func()) *paneSurface {
	s := &paneSurface{content: content, onActivate: onActivate}
	s.ExtendBaseWidget(s)
	return s
}

func (s *paneSurface) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(s.content)
}

func (s *paneSurface) Tapped(_ *fyne.PointEvent) {
	if s.onActivate != nil {
		s.onActivate()
	}
}

func (s *paneSurface) TappedSecondary(_ *fyne.PointEvent) {
	if s.onActivate != nil {
		s.onActivate()
	}
}

func (s *paneSurface) MouseIn(_ *desktop.MouseEvent) {
	if s.onActivate != nil {
		s.onActivate()
	}
}

func (s *paneSurface) MouseOut()                        {}
func (s *paneSurface) MouseMoved(_ *desktop.MouseEvent) {}

func newChatPane(onSend func(*ChatPane, string, *models.Message), onFocused func(*ChatPane), onInputShortcut func(fyne.Shortcut) bool) *ChatPane {
	p := &ChatPane{id: paneIDCounter}
	paneIDCounter++

	p.msgView = NewMessageView(func(msg models.Message) {
		onFocused(p)
		p.inputArea.SetReplyTarget(msg)
	}, func() {
		onFocused(p)
	})
	p.inputArea = NewInputAreaWithShortcutHandler(
		func(text string, replyTo *models.Message) { onSend(p, text, replyTo) },
		func() { onFocused(p) },
		onInputShortcut,
	)
	gapBelow := canvas.NewRectangle(color.Transparent)
	gapBelow.SetMinSize(fyne.NewSize(1, inputBottomGapHeight()))
	inputWithGap := container.NewVBox(p.inputArea.Widget(), gapBelow)
	p.inputVisible = true
	p.widget = container.NewMax()
	p.widget.Objects = []fyne.CanvasObject{container.NewBorder(nil, inputWithGap, nil, nil, p.msgView.Widget())}
	p.surface = newPaneSurface(p.widget, func() { onFocused(p) })
	return p
}

// Widget returns the full pane canvas object.
func (p *ChatPane) Widget() fyne.CanvasObject { return p.surface }

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

// SetInputVisible toggles whether the pane's input box is rendered.
func (p *ChatPane) SetInputVisible(visible bool) {
	if p.widget == nil {
		return
	}
	if p.inputVisible == visible {
		return
	}
	p.inputVisible = visible
	p.rebuildLayout(false)
	p.msgView.ScrollToBottom()
}

// RefreshLayout rebuilds pane spacing/layout while preserving input visibility state.
func (p *ChatPane) RefreshLayout() {
	if p.widget == nil {
		return
	}
	p.rebuildLayout(false)
}

func (p *ChatPane) rebuildLayout(reveal bool) {
	var bottom fyne.CanvasObject
	if p.inputVisible {
		gapBelow := canvas.NewRectangle(color.Transparent)
		gapBelow.SetMinSize(fyne.NewSize(1, inputBottomGapHeight()))
		bottom = container.NewVBox(p.inputArea.Widget(), gapBelow)

		p.widget.Objects = []fyne.CanvasObject{
			container.NewBorder(nil, bottom, nil, nil, p.msgView.Widget()),
		}
		p.widget.Refresh()

		if reveal {
			// Kept for API compatibility; we intentionally avoid extra gap animation
			// to ensure a stable single-line spacing between messages and input.
		}
	} else {
		hiddenSpacer := canvas.NewRectangle(color.Transparent)
		hiddenSpacer.SetMinSize(fyne.NewSize(1, hiddenInputSpacerHeight()))
		bottom = hiddenSpacer

		p.widget.Objects = []fyne.CanvasObject{
			container.NewBorder(nil, bottom, nil, nil, p.msgView.Widget()),
		}
		p.widget.Refresh()
		return
	}

	p.widget.Objects = []fyne.CanvasObject{
		container.NewBorder(nil, bottom, nil, nil, p.msgView.Widget()),
	}
	p.widget.Refresh()
}
