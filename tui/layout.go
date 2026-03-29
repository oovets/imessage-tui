package tui

// SplitDirection defines how a layout node splits its space
type SplitDirection int

const (
	SplitNone       SplitDirection = iota // Leaf node (contains a window)
	SplitHorizontal                       // Left | Right (side by side)
	SplitVertical                         // Top / Bottom (stacked)
)

// LayoutNode represents a node in the binary layout tree
type LayoutNode struct {
	Direction  SplitDirection
	Window     *ChatWindow // Only set when Direction == SplitNone
	Left       *LayoutNode // Left or Top child
	Right      *LayoutNode // Right or Bottom child
	SplitRatio float64     // Ratio for split (0.5 = 50/50)

	// Calculated bounds
	x, y, width, height int
}

// NewLeafNode creates a leaf node containing a window
func NewLeafNode(window *ChatWindow) *LayoutNode {
	return &LayoutNode{
		Direction:  SplitNone,
		Window:     window,
		SplitRatio: 0.5,
	}
}

// NewSplitNode creates a split node with two children
func NewSplitNode(direction SplitDirection, left, right *LayoutNode) *LayoutNode {
	return &LayoutNode{
		Direction:  direction,
		Left:       left,
		Right:      right,
		SplitRatio: 0.5,
	}
}

// IsLeaf returns true if this is a leaf node (contains a window)
func (n *LayoutNode) IsLeaf() bool {
	return n.Direction == SplitNone
}

// CalculateLayout recursively calculates bounds for all nodes
func (n *LayoutNode) CalculateLayout(x, y, w, h int) {
	n.x = x
	n.y = y
	n.width = w
	n.height = h

	if n.Direction == SplitNone {
		// Leaf node: set window bounds
		if n.Window != nil {
			n.Window.SetBounds(x, y, w, h)
		}
		return
	}

	if n.Direction == SplitHorizontal {
		// Split horizontally: left | right
		leftW, rightW, dividerW := splitAxis(w, n.SplitRatio)
		if n.Left != nil {
			n.Left.CalculateLayout(x, y, leftW, h)
		}
		if n.Right != nil {
			n.Right.CalculateLayout(x+leftW+dividerW, y, rightW, h)
		}
	} else {
		// Split vertically: top / bottom
		topH, bottomH, dividerH := splitAxis(h, n.SplitRatio)
		if n.Left != nil {
			n.Left.CalculateLayout(x, y, w, topH)
		}
		if n.Right != nil {
			n.Right.CalculateLayout(x, y+topH+dividerH, w, bottomH)
		}
	}
}

// splitAxis returns first, second, divider sizes for a split dimension.
// It keeps both panes visible when possible and avoids overshooting the parent size.
func splitAxis(total int, ratio float64) (int, int, int) {
	if total <= 0 {
		return 0, 0, 0
	}

	divider := 0
	usable := total
	if total >= 3 {
		divider = 1
		usable = total - divider
	}

	if usable <= 1 {
		return usable, 0, divider
	}

	if ratio < 0.1 {
		ratio = 0.1
	} else if ratio > 0.9 {
		ratio = 0.9
	}

	first := int(float64(usable) * ratio)
	if first < 1 {
		first = 1
	}
	if first > usable-1 {
		first = usable - 1
	}
	second := usable - first
	return first, second, divider
}

// FindWindow finds the window with the given ID in the tree
func (n *LayoutNode) FindWindow(id WindowID) *ChatWindow {
	if n.Direction == SplitNone {
		if n.Window != nil && n.Window.ID == id {
			return n.Window
		}
		return nil
	}

	if found := n.Left.FindWindow(id); found != nil {
		return found
	}
	return n.Right.FindWindow(id)
}

// FindNodeWithWindow finds the node containing the window with given ID
func (n *LayoutNode) FindNodeWithWindow(id WindowID) *LayoutNode {
	if n.Direction == SplitNone {
		if n.Window != nil && n.Window.ID == id {
			return n
		}
		return nil
	}

	if found := n.Left.FindNodeWithWindow(id); found != nil {
		return found
	}
	return n.Right.FindNodeWithWindow(id)
}

// CountWindows returns the number of windows in the tree
func (n *LayoutNode) CountWindows() int {
	if n.Direction == SplitNone {
		if n.Window != nil {
			return 1
		}
		return 0
	}

	return n.Left.CountWindows() + n.Right.CountWindows()
}

// AllWindows returns all windows in the tree
func (n *LayoutNode) AllWindows() []*ChatWindow {
	if n.Direction == SplitNone {
		if n.Window != nil {
			return []*ChatWindow{n.Window}
		}
		return nil
	}

	windows := n.Left.AllWindows()
	windows = append(windows, n.Right.AllWindows()...)
	return windows
}

// ReplaceWindow replaces a leaf node's window with a split containing two windows
func (n *LayoutNode) ReplaceWindow(targetID WindowID, direction SplitDirection, newWindow *ChatWindow) bool {
	if n.Direction == SplitNone {
		if n.Window != nil && n.Window.ID == targetID {
			// This is the target - transform into a split
			n.Direction = direction
			n.Left = NewLeafNode(n.Window)
			n.Right = NewLeafNode(newWindow)
			n.Window = nil
			n.SplitRatio = 0.5
			return true
		}
		return false
	}

	// Search in children
	if n.Left.ReplaceWindow(targetID, direction, newWindow) {
		return true
	}
	return n.Right.ReplaceWindow(targetID, direction, newWindow)
}

// RemoveWindow removes a window and collapses the tree
// Returns the remaining node if successful, nil if the window wasn't found
func (n *LayoutNode) RemoveWindow(targetID WindowID) *LayoutNode {
	if n.Direction == SplitNone {
		// Can't remove from a leaf - this is handled by parent
		return nil
	}

	// Check if left child contains the target
	if n.Left.Direction == SplitNone && n.Left.Window != nil && n.Left.Window.ID == targetID {
		// Remove left, return right
		return n.Right
	}

	// Check if right child contains the target
	if n.Right.Direction == SplitNone && n.Right.Window != nil && n.Right.Window.ID == targetID {
		// Remove right, return left
		return n.Left
	}

	// Recursively check children
	if newLeft := n.Left.RemoveWindow(targetID); newLeft != nil {
		n.Left = newLeft
		return n
	}
	if newRight := n.Right.RemoveWindow(targetID); newRight != nil {
		n.Right = newRight
		return n
	}

	return nil
}

// GetBounds returns the calculated bounds of this node
func (n *LayoutNode) GetBounds() (x, y, width, height int) {
	return n.x, n.y, n.width, n.height
}
