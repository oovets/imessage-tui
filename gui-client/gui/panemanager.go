package gui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oovets/bluebubbles-gui/models"
)

type splitDir int

const (
	splitHorizontal splitDir = iota
	splitVertical
	defaultMaxPanes = 8
)

// paneNode is a node in a binary tree of chat panes.
// A leaf node (pane != nil) wraps a single ChatPane.
// An internal node (pane == nil) splits two children.
type paneNode struct {
	// Leaf
	pane *ChatPane

	// Internal
	left  *paneNode
	right *paneNode
	dir   splitDir
}

func (n *paneNode) isLeaf() bool { return n.pane != nil }

// buildWidget recursively constructs the Fyne layout for this subtree.
func (n *paneNode) buildWidget() fyne.CanvasObject {
	if n.isLeaf() {
		return n.pane.Widget()
	}
	left := n.left.buildWidget()
	right := n.right.buildWidget()
	if n.dir == splitHorizontal {
		s := container.NewHSplit(left, right)
		s.SetOffset(0.5)
		return newSplitWithoutDivider(s, splitHorizontal)
	}
	s := container.NewVSplit(left, right)
	s.SetOffset(0.5)
	return newSplitWithoutDivider(s, splitVertical)
}

// allPanes returns all ChatPane leaves in tree order.
func (n *paneNode) allPanes() []*ChatPane {
	if n.isLeaf() {
		return []*ChatPane{n.pane}
	}
	return append(n.left.allPanes(), n.right.allPanes()...)
}

// split finds the leaf containing target and converts it to an internal node
// with target on the left and newPane on the right.
func (n *paneNode) split(target *ChatPane, newPane *ChatPane, dir splitDir) bool {
	if n.isLeaf() {
		if n.pane == target {
			n.left = &paneNode{pane: n.pane}
			n.right = &paneNode{pane: newPane}
			n.dir = dir
			n.pane = nil
			return true
		}
		return false
	}
	return n.left.split(target, newPane, dir) || n.right.split(target, newPane, dir)
}

// remove finds the leaf containing target, removes it, and replaces its parent
// with the sibling. The struct at n is mutated in place.
func (n *paneNode) remove(target *ChatPane) bool {
	if n.isLeaf() {
		return false
	}
	if n.left.isLeaf() && n.left.pane == target {
		*n = *n.right // replace this node with right sibling
		return true
	}
	if n.right.isLeaf() && n.right.pane == target {
		*n = *n.left // replace this node with left sibling
		return true
	}
	return n.left.remove(target) || n.right.remove(target)
}

// PaneManager manages a binary tree of ChatPanes and the Fyne container
// that holds the rendered split layout. All methods must be called from
// the Fyne main goroutine.
type PaneManager struct {
	root       paneNode
	focused    *ChatPane
	holder     *fyne.Container
	maxPanes   int
	appFocused bool

	onSend          func(*ChatPane, string, *models.Message)
	onReact         func(*ChatPane, models.Message, string)
	onFocused       func(*ChatPane)
	onInputShortcut func(fyne.Shortcut) bool
}

type paneManagerState struct {
	Root          *paneStateNode `json:"root"`
	FocusedPaneID int            `json:"focusedPaneID"`
}

type paneStateNode struct {
	PaneID   int            `json:"paneID,omitempty"`
	ChatGUID string         `json:"chatGUID,omitempty"`
	Dir      string         `json:"dir,omitempty"`
	Left     *paneStateNode `json:"left,omitempty"`
	Right    *paneStateNode `json:"right,omitempty"`
}

func NewPaneManager(onSend func(*ChatPane, string, *models.Message), onReact func(*ChatPane, models.Message, string), onFocused func(*ChatPane), onInputShortcut func(fyne.Shortcut) bool) *PaneManager {
	pm := &PaneManager{
		onSend: onSend, onReact: onReact, onFocused: onFocused, onInputShortcut: onInputShortcut,
		maxPanes: defaultMaxPanes, appFocused: true,
	}

	first := pm.newPane()
	pm.root = paneNode{pane: first}
	pm.focused = first
	first.SetFocused(true)

	pm.holder = container.NewMax(pm.root.buildWidget())
	return pm
}

func (pm *PaneManager) newPane() *ChatPane {
	p := newChatPane(
		pm.onSend,
		pm.onReact,
		func(p *ChatPane) {
			if pm.focused != p {
				if pm.focused != nil {
					pm.focused.SetFocused(false)
				}
				pm.focused = p
				p.SetFocused(true)
			}
			pm.syncInputVisibility()
			pm.onFocused(p)
		},
		pm.onInputShortcut,
	)
	return p
}

// Widget returns the holder container embedded in the main window layout.
func (pm *PaneManager) Widget() fyne.CanvasObject { return pm.holder }

// FocusedPane returns the currently focused pane.
func (pm *PaneManager) FocusedPane() *ChatPane { return pm.focused }

// AppFocused reports whether the app window is currently focused/foregrounded.
func (pm *PaneManager) AppFocused() bool { return pm.appFocused }

// AllPanes returns all panes in tree order.
func (pm *PaneManager) AllPanes() []*ChatPane { return pm.root.allPanes() }

// PanesShowingChat returns all panes whose ChatGUID matches guid.
func (pm *PaneManager) PanesShowingChat(guid string) []*ChatPane {
	var out []*ChatPane
	for _, p := range pm.AllPanes() {
		if p.ChatGUID == guid {
			out = append(out, p)
		}
	}
	return out
}

// SplitFocused splits the focused pane in the given direction.
func (pm *PaneManager) SplitFocused(dir splitDir) {
	if pm.focused == nil || len(pm.AllPanes()) >= pm.maxPanes {
		return
	}
	newPane := pm.newPane()
	pm.root.split(pm.focused, newPane, dir)

	if pm.focused != nil {
		pm.focused.SetFocused(false)
	}
	pm.focused = newPane
	newPane.SetFocused(true)
	pm.syncInputVisibility()

	pm.rebuildHolder()
}

// IsFocusedInputActive reports whether the focused pane currently has an active input field.
func (pm *PaneManager) IsFocusedInputActive() bool {
	if pm.focused == nil {
		return false
	}
	return pm.focused.IsInputFocused()
}

// CloseFocused removes the focused pane (minimum 1 pane is kept).
func (pm *PaneManager) CloseFocused() {
	panes := pm.AllPanes()
	if len(panes) <= 1 || pm.focused == nil {
		return
	}

	// Remember which pane to focus next (previous in list, or first)
	var next *ChatPane
	for i, p := range panes {
		if p == pm.focused {
			if i > 0 {
				next = panes[i-1]
			} else {
				next = panes[1]
			}
			break
		}
	}

	pm.root.remove(pm.focused)
	pm.focused.SetFocused(false)
	pm.focused = next
	if next != nil {
		next.SetFocused(true)
	}
	pm.syncInputVisibility()

	pm.rebuildHolder()
}

// SetFocus explicitly focuses the given pane.
func (pm *PaneManager) SetFocus(pane *ChatPane) {
	if pm.focused == pane {
		return
	}
	if pm.focused != nil {
		pm.focused.SetFocused(false)
	}
	pm.focused = pane
	pane.SetFocused(true)
	pm.syncInputVisibility()
}

func (pm *PaneManager) rebuildHolder() {
	pm.holder.Objects = []fyne.CanvasObject{pm.root.buildWidget()}
	pm.holder.Refresh()
}

// SetAppFocused updates app focus state and syncs pane input visibility.
func (pm *PaneManager) SetAppFocused(focused bool) {
	if pm.appFocused == focused {
		return
	}
	pm.appFocused = focused
	pm.syncInputVisibility()
}

func (pm *PaneManager) syncInputVisibility() {
	for _, p := range pm.AllPanes() {
		p.SetFocused(pm.appFocused && p == pm.focused)
		if !pm.appFocused {
			// Hide all input cards when the app window loses focus.
			p.SetInputVisible(false)
		}
	}
}

// SerializeState returns a JSON snapshot of split layout, focused pane and chat assignments.
func (pm *PaneManager) SerializeState() (string, error) {
	state := paneManagerState{
		Root:          serializePaneNode(&pm.root),
		FocusedPaneID: -1,
	}
	if pm.focused != nil {
		state.FocusedPaneID = pm.focused.id
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// RestoreState restores split layout, focused pane and chat assignments from JSON.
func (pm *PaneManager) RestoreState(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var state paneManagerState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return err
	}
	if state.Root == nil {
		return fmt.Errorf("invalid pane state: missing root")
	}

	idMap := make(map[int]*ChatPane)
	root, err := pm.restorePaneNode(state.Root, idMap)
	if err != nil {
		return err
	}
	pm.root = *root

	for _, p := range pm.AllPanes() {
		p.SetFocused(false)
	}
	pm.focused = idMap[state.FocusedPaneID]
	if pm.focused == nil {
		panes := pm.AllPanes()
		if len(panes) > 0 {
			pm.focused = panes[0]
		}
	}

	pm.rebuildHolder()
	pm.syncInputVisibility()
	paneIDCounter = maxPaneID(pm.AllPanes()) + 1
	return nil
}

func serializePaneNode(n *paneNode) *paneStateNode {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return &paneStateNode{
			PaneID:   n.pane.id,
			ChatGUID: n.pane.ChatGUID,
		}
	}
	dir := "horizontal"
	if n.dir == splitVertical {
		dir = "vertical"
	}
	return &paneStateNode{
		Dir:   dir,
		Left:  serializePaneNode(n.left),
		Right: serializePaneNode(n.right),
	}
}

func (pm *PaneManager) restorePaneNode(n *paneStateNode, idMap map[int]*ChatPane) (*paneNode, error) {
	if n == nil {
		return nil, fmt.Errorf("invalid pane node")
	}
	if n.Left == nil && n.Right == nil {
		p := pm.newPane()
		p.ChatGUID = n.ChatGUID
		p.id = n.PaneID
		idMap[p.id] = p
		return &paneNode{pane: p}, nil
	}
	if n.Left == nil || n.Right == nil {
		return nil, fmt.Errorf("invalid split node")
	}
	left, err := pm.restorePaneNode(n.Left, idMap)
	if err != nil {
		return nil, err
	}
	right, err := pm.restorePaneNode(n.Right, idMap)
	if err != nil {
		return nil, err
	}
	var dir splitDir
	switch strings.ToLower(n.Dir) {
	case "horizontal":
		dir = splitHorizontal
	case "vertical":
		dir = splitVertical
	default:
		return nil, fmt.Errorf("invalid split dir: %q", n.Dir)
	}
	return &paneNode{left: left, right: right, dir: dir}, nil
}

func maxPaneID(panes []*ChatPane) int {
	maxID := -1
	for _, p := range panes {
		if p != nil && p.id > maxID {
			maxID = p.id
		}
	}
	return maxID
}

type splitWithoutDivider struct {
	widget.BaseWidget
	split *container.Split
	dir   splitDir
	mask  *canvas.Rectangle
}

func newSplitWithoutDivider(split *container.Split, dir splitDir) *splitWithoutDivider {
	w := &splitWithoutDivider{
		split: split,
		dir:   dir,
		mask:  canvas.NewRectangle(color.Transparent),
	}
	w.mask.StrokeWidth = 0
	w.ExtendBaseWidget(w)
	return w
}

func (w *splitWithoutDivider) CreateRenderer() fyne.WidgetRenderer {
	return &splitWithoutDividerRenderer{w: w, objs: []fyne.CanvasObject{w.split, w.mask}}
}

type splitWithoutDividerRenderer struct {
	w    *splitWithoutDivider
	objs []fyne.CanvasObject
}

func (r *splitWithoutDividerRenderer) Layout(size fyne.Size) {
	r.w.split.Resize(size)
	r.w.split.Move(fyne.NewPos(0, 0))
	offset := float32(r.w.split.Offset)
	if offset < 0 {
		offset = 0
	}
	if offset > 1 {
		offset = 1
	}
	thickness := float32(theme.Padding())*2 + 2
	if thickness < 1 {
		thickness = 1
	}
	if r.w.dir == splitHorizontal {
		x := size.Width*offset - thickness/2
		if x < 0 {
			x = 0
		}
		if x+thickness > size.Width {
			x = size.Width - thickness
		}
		r.w.mask.Move(fyne.NewPos(x, 0))
		r.w.mask.Resize(fyne.NewSize(thickness+3, size.Height))
		return
	}
	y := size.Height*offset - thickness/2
	if y < 0 {
		y = 0
	}
	if y+thickness > size.Height {
		y = size.Height - thickness
	}
	r.w.mask.Move(fyne.NewPos(0, y))
	r.w.mask.Resize(fyne.NewSize(size.Width, thickness+3))
}

func (r *splitWithoutDividerRenderer) MinSize() fyne.Size {
	return r.w.split.MinSize()
}

func (r *splitWithoutDividerRenderer) Refresh() {
	r.w.mask.FillColor = theme.Color(theme.ColorNameBackground)
	r.Layout(r.w.Size())
	canvas.Refresh(r.w.mask)
	canvas.Refresh(r.w.split)
}

func (r *splitWithoutDividerRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *splitWithoutDividerRenderer) Destroy()                     {}

func colorToNRGBA(c color.Color) color.NRGBA {
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
