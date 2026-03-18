package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

type splitDir int

const (
	splitHorizontal splitDir = iota
	splitVertical
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
		return s
	}
	s := container.NewVSplit(left, right)
	s.SetOffset(0.5)
	return s
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
	root    paneNode
	focused *ChatPane
	holder  *fyne.Container

	onSend    func(*ChatPane, string)
	onFocused func(*ChatPane)
}

func NewPaneManager(onSend func(*ChatPane, string), onFocused func(*ChatPane)) *PaneManager {
	pm := &PaneManager{onSend: onSend, onFocused: onFocused}

	first := pm.newPane()
	pm.root = paneNode{pane: first}
	pm.focused = first
	first.SetFocused(true)

	pm.holder = container.NewMax(pm.root.buildWidget())
	return pm
}

func (pm *PaneManager) newPane() *ChatPane {
	return newChatPane(
		pm.onSend,
		func(p *ChatPane) {
			if pm.focused != p {
				if pm.focused != nil {
					pm.focused.SetFocused(false)
				}
				pm.focused = p
				p.SetFocused(true)
			}
			pm.onFocused(p)
		},
	)
}

// Widget returns the holder container embedded in the main window layout.
func (pm *PaneManager) Widget() fyne.CanvasObject { return pm.holder }

// FocusedPane returns the currently focused pane.
func (pm *PaneManager) FocusedPane() *ChatPane { return pm.focused }

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

// SplitFocused splits the focused pane in the given direction (max 4 panes).
func (pm *PaneManager) SplitFocused(dir splitDir) {
	if pm.focused == nil || len(pm.AllPanes()) >= 4 {
		return
	}
	newPane := pm.newPane()
	pm.root.split(pm.focused, newPane, dir)

	if pm.focused != nil {
		pm.focused.SetFocused(false)
	}
	pm.focused = newPane
	newPane.SetFocused(true)

	pm.rebuildHolder()
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
}

func (pm *PaneManager) rebuildHolder() {
	pm.holder.Objects = []fyne.CanvasObject{pm.root.buildWidget()}
	pm.holder.Refresh()
}
