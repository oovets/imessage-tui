package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oovets/bluebubbles-gui/models"
)

var paneIDCounter int

// floatingBottomLayout places objects[0] at full container size and objects[1]
// as an overlay card anchored to the bottom with horizontal and bottom padding.
// The background (msgView) is never resized by the card — true overlay.
type floatingBottomLayout struct {
	hPad float32
	bPad float32
}

func (l *floatingBottomLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(size)
	if len(objs) < 2 || !objs[1].Visible() {
		return
	}
	cardW := size.Width - 2*l.hPad
	if cardW < 0 {
		cardW = 0
	}
	cardH := objs[1].MinSize().Height
	objs[1].Resize(fyne.NewSize(cardW, cardH))
	objs[1].Move(fyne.NewPos(l.hPad, size.Height-cardH-l.bPad))
}

func (l *floatingBottomLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	// A small constant keeps the layout from advertising the scroll content's
	// full minimum width (which could be very wide) to parent containers.
	return fyne.NewSize(80, 80)
}

// ChatPane is a single panel showing one conversation.
// The input card sits at the bottom in a border layout.
type ChatPane struct {
	id        int
	msgView   *MessageView
	inputArea *InputArea
	ChatGUID  string
	widget    fyne.CanvasObject
	surface   *paneSurface
}

type paneSurface struct {
	widget.BaseWidget
	content    fyne.CanvasObject
	onActivate func()
	onResize   func()
}

func newPaneSurface(content fyne.CanvasObject, onActivate func(), onResize func()) *paneSurface {
	s := &paneSurface{content: content, onActivate: onActivate, onResize: onResize}
	s.ExtendBaseWidget(s)
	return s
}

func (s *paneSurface) MinSize() fyne.Size {
	// Return a small constant so Fyne's split containers are not forced to
	// honour the full content MinSize (which can be hundreds of dp wide when
	// messages contain long words, filenames, or images). Content will still
	// be laid out at whatever width the split actually allocates.
	return fyne.NewSize(80, 80)
}

func (s *paneSurface) CreateRenderer() fyne.WidgetRenderer {
	return &paneSurfaceRenderer{surface: s}
}

type paneSurfaceRenderer struct {
	surface  *paneSurface
	lastSize fyne.Size
}

func (r *paneSurfaceRenderer) Layout(size fyne.Size) {
	r.surface.content.Move(fyne.NewPos(0, 0))
	r.surface.content.Resize(size)
	if size != r.lastSize {
		r.lastSize = size
		if r.surface.onResize != nil {
			r.surface.onResize()
		}
	}
}

func (r *paneSurfaceRenderer) MinSize() fyne.Size {
	return r.surface.content.MinSize()
}

func (r *paneSurfaceRenderer) Refresh() {
	canvas.Refresh(r.surface.content)
}

func (r *paneSurfaceRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.surface.content}
}

func (r *paneSurfaceRenderer) Destroy() {}

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

func (s *paneSurface) MouseIn(_ *desktop.MouseEvent)    {}
func (s *paneSurface) MouseOut()                        {}
func (s *paneSurface) MouseMoved(_ *desktop.MouseEvent) {}

func newChatPane(onSend func(*ChatPane, string, *models.Message), onReact func(*ChatPane, models.Message, string), onFocused func(*ChatPane), onInputShortcut func(fyne.Shortcut) bool) *ChatPane {
	p := &ChatPane{id: paneIDCounter}
	paneIDCounter++

	p.msgView = NewMessageView(func(msg models.Message) {
		onFocused(p)
		p.inputArea.SetReplyTarget(msg)
	}, func(msg models.Message, reaction string) {
		onFocused(p)
		if onReact != nil {
			onReact(p, msg, reaction)
		}
	})

	p.inputArea = NewInputAreaWithShortcutHandler(
		func(text string, replyTo *models.Message) { onSend(p, text, replyTo) },
		func() { onFocused(p) },
		onInputShortcut,
	)
	p.inputArea.SetOnLayoutChanged(func() {
		p.refreshBottomInset()
		if p.surface != nil {
			p.surface.Refresh()
		}
	})

	// Floating layout: message view fills the pane and the input box sits above it.
	p.widget = container.New(&floatingBottomLayout{hPad: 16, bPad: 12}, p.msgView.Widget(), p.inputArea.Widget())
	p.refreshBottomInset()

	p.surface = newPaneSurface(p.widget, func() { onFocused(p) }, func() { p.msgView.ScrollToBottom() })
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

// SetInputVisible is kept for PaneManager compatibility; ignored since the
// input card is always visible.
func (p *ChatPane) SetInputVisible(_ bool) {}

// RefreshLayout updates theme-sensitive colours and sizes.
func (p *ChatPane) RefreshLayout() {
	p.inputArea.RefreshLayout()
	p.refreshBottomInset()
}

func (p *ChatPane) refreshBottomInset() {
	if p == nil || p.msgView == nil || p.inputArea == nil {
		return
	}
	pad := p.inputArea.Widget().MinSize().Height + float32(theme.TextSize()) + 12
	p.msgView.SetBottomPad(pad)
}
