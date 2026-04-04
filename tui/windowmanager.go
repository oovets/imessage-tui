package tui

import (
	"strings"

	"github.com/oovets/bluebubbles-tui/models"
	"github.com/charmbracelet/lipgloss"
)

// Direction for focus navigation
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// WindowManager manages multiple chat windows and their layout
type WindowManager struct {
	root           *LayoutNode
	windows        map[WindowID]*ChatWindow
	nextID         WindowID
	focusedWindow  WindowID
	maxWindows      int
	showTimestamps  bool
	showLineNumbers bool

	// Message cache per chat GUID
	messageCache     map[string][]models.Message
	messageCacheGUID map[string]map[string]struct{}

	// Available dimensions
	width, height int
}

// NewWindowManager creates a new window manager with a single window
func NewWindowManager() *WindowManager {
	wm := &WindowManager{
		windows:          make(map[WindowID]*ChatWindow),
		nextID:           1,
		maxWindows:       4,
		messageCache:     make(map[string][]models.Message),
		messageCacheGUID: make(map[string]map[string]struct{}),
		showTimestamps:   true,
		showLineNumbers:  true,
	}

	// Create initial window
	window := NewChatWindow(0)
	window.Messages.SetShowTimestamps(wm.showTimestamps)
	window.Messages.SetShowLineNumbers(wm.showLineNumbers)
	window.Focused = true
	wm.windows[0] = window
	wm.focusedWindow = 0
	wm.root = NewLeafNode(window)

	return wm
}

// SetSize updates the available dimensions and recalculates layout
func (wm *WindowManager) SetSize(width, height int) {
	wm.width = width
	wm.height = height
	wm.recalculateLayout()
}

// recalculateLayout updates all window bounds based on current size
func (wm *WindowManager) recalculateLayout() {
	if wm.root != nil && wm.width > 0 && wm.height > 0 {
		wm.root.CalculateLayout(0, 0, wm.width, wm.height)
	}
}

// FocusedWindow returns the currently focused window
func (wm *WindowManager) FocusedWindow() *ChatWindow {
	return wm.windows[wm.focusedWindow]
}

// SetFocus sets focus to a specific window
func (wm *WindowManager) SetFocus(id WindowID) {
	// Clear old focus
	if old, ok := wm.windows[wm.focusedWindow]; ok {
		old.Focused = false
		old.Input.Blur()
	}

	// Set new focus
	wm.focusedWindow = id
	if window, ok := wm.windows[id]; ok {
		window.Focused = true
	}
}

// CycleFocus moves focus to the next window (ordered by ID).
// Returns true if it moved to another window.
// Returns false if it wrapped around (was on last window) or only one window exists.
func (wm *WindowManager) CycleFocus() bool {
	if len(wm.windows) <= 1 {
		return false
	}

	// Collect and sort window IDs
	ids := make([]WindowID, 0, len(wm.windows))
	for id := range wm.windows {
		ids = append(ids, id)
	}
	for i := range ids {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}

	// Find current position
	for i, id := range ids {
		if id == wm.focusedWindow {
			nextIdx := i + 1
			if nextIdx >= len(ids) {
				// Wrapped around — signal caller to go back to chat list
				return false
			}
			wm.SetFocus(ids[nextIdx])
			return true
		}
	}

	return false
}

// SplitWindow splits the focused window in the given direction
// Returns true if split was successful
func (wm *WindowManager) SplitWindow(direction SplitDirection) bool {
	// Check max windows
	if len(wm.windows) >= wm.maxWindows {
		return false
	}

	// Create new window
	newWindow := NewChatWindow(wm.nextID)
	newWindow.Messages.SetShowTimestamps(wm.showTimestamps)
	newWindow.Messages.SetShowLineNumbers(wm.showLineNumbers)
	wm.windows[wm.nextID] = newWindow
	wm.nextID++

	// Split the focused window
	if wm.root.CountWindows() == 1 {
		// Single window - transform root
		wm.root.Direction = direction
		wm.root.Left = NewLeafNode(wm.windows[wm.focusedWindow])
		wm.root.Right = NewLeafNode(newWindow)
		wm.root.Window = nil
		wm.root.SplitRatio = 0.5
	} else {
		// Multiple windows - find and replace target
		wm.root.ReplaceWindow(wm.focusedWindow, direction, newWindow)
	}

	// Recalculate layout
	wm.recalculateLayout()

	// Focus new window
	wm.SetFocus(newWindow.ID)

	return true
}

// CloseWindow closes the focused window
// Returns true if closed, false if it's the last window
func (wm *WindowManager) CloseWindow() bool {
	// Don't close the last window
	if len(wm.windows) <= 1 {
		return false
	}

	closingID := wm.focusedWindow

	// Find another window to focus
	var newFocusID WindowID
	for id := range wm.windows {
		if id != closingID {
			newFocusID = id
			break
		}
	}

	// Remove window from tree
	if wm.root.CountWindows() == 2 {
		// Will become single window - find the remaining one
		remaining := wm.root.Left.AllWindows()
		if len(remaining) == 1 && remaining[0].ID == closingID {
			remaining = wm.root.Right.AllWindows()
		}
		if len(remaining) > 0 {
			wm.root = NewLeafNode(remaining[0])
		}
	} else {
		// More than 2 windows - collapse tree
		newRoot := wm.root.RemoveWindow(closingID)
		if newRoot != nil {
			wm.root = newRoot
		}
	}

	// Remove from map
	delete(wm.windows, closingID)

	// Focus new window
	wm.SetFocus(newFocusID)

	// Recalculate layout
	wm.recalculateLayout()

	return true
}

// FocusDirection moves focus in the given direction
func (wm *WindowManager) FocusDirection(dir Direction) {
	current := wm.windows[wm.focusedWindow]
	if current == nil {
		return
	}

	// Get center of current window
	cx := current.x + current.width/2
	cy := current.y + current.height/2

	var best *ChatWindow
	bestDist := -1

	for id, window := range wm.windows {
		if id == wm.focusedWindow {
			continue
		}

		// Get center of candidate window
		wx := window.x + window.width/2
		wy := window.y + window.height/2

		// Check if window is in the right direction
		var inDirection bool
		switch dir {
		case DirLeft:
			inDirection = wx < cx
		case DirRight:
			inDirection = wx > cx
		case DirUp:
			inDirection = wy < cy
		case DirDown:
			inDirection = wy > cy
		}

		if !inDirection {
			continue
		}

		// Calculate Manhattan distance
		dist := abs(wx-cx) + abs(wy-cy)
		if best == nil || dist < bestDist {
			best = window
			bestDist = dist
		}
	}

	if best != nil {
		wm.SetFocus(best.ID)
	}
}

// CacheMessage adds a message to the cache for a chat.
// Returns true if the message was added, false if it was a duplicate.
func (wm *WindowManager) CacheMessage(chatGUID string, msg models.Message) bool {
	if msg.GUID != "" {
		if _, ok := wm.messageCacheGUID[chatGUID]; !ok {
			wm.messageCacheGUID[chatGUID] = make(map[string]struct{})
		}
		if _, exists := wm.messageCacheGUID[chatGUID][msg.GUID]; exists {
			return false
		}
		wm.messageCacheGUID[chatGUID][msg.GUID] = struct{}{}
	}

	wm.messageCache[chatGUID] = append(wm.messageCache[chatGUID], msg)
	return true
}

// GetCachedMessages returns cached messages for a chat
func (wm *WindowManager) GetCachedMessages(chatGUID string) []models.Message {
	return wm.messageCache[chatGUID]
}

// SetCachedMessages sets the cached messages for a chat
func (wm *WindowManager) SetCachedMessages(chatGUID string, messages []models.Message) {
	wm.messageCache[chatGUID] = messages
	idx := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		if msg.GUID != "" {
			idx[msg.GUID] = struct{}{}
		}
	}
	wm.messageCacheGUID[chatGUID] = idx
}

// WindowsShowingChat returns all windows displaying a specific chat
func (wm *WindowManager) WindowsShowingChat(chatGUID string) []*ChatWindow {
	var result []*ChatWindow
	for _, window := range wm.windows {
		if window.Chat != nil && window.Chat.GUID == chatGUID {
			result = append(result, window)
		}
	}
	return result
}

// AllWindows returns all windows
func (wm *WindowManager) AllWindows() []*ChatWindow {
	result := make([]*ChatWindow, 0, len(wm.windows))
	for _, w := range wm.windows {
		result = append(result, w)
	}
	return result
}

// WindowCount returns the number of windows
func (wm *WindowManager) WindowCount() int {
	return len(wm.windows)
}

// SetShowTimestamps toggles timestamps for all windows.
func (wm *WindowManager) SetShowTimestamps(show bool) {
	if wm.showTimestamps == show {
		return
	}
	wm.showTimestamps = show
	for _, w := range wm.windows {
		w.Messages.SetShowTimestamps(show)
	}
}

// SetShowLineNumbers toggles line numbers for all windows.
func (wm *WindowManager) SetShowLineNumbers(show bool) {
	if wm.showLineNumbers == show {
		return
	}
	wm.showLineNumbers = show
	for _, w := range wm.windows {
		w.Messages.SetShowLineNumbers(show)
	}
}

// Render renders all windows
func (wm *WindowManager) Render() string {
	if wm.root == nil || wm.width == 0 || wm.height == 0 {
		return ""
	}

	return wm.renderNode(wm.root)
}

// renderNode recursively renders a layout node
func (wm *WindowManager) renderNode(node *LayoutNode) string {
	if node.IsLeaf() {
		if node.Window != nil {
			return node.Window.View()
		}
		return ""
	}

	leftView := wm.renderNode(node.Left)
	rightView := wm.renderNode(node.Right)

	if node.Direction == SplitHorizontal {
		_, _, dividerW := splitAxis(node.width, node.SplitRatio)
		if dividerW == 0 {
			return lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
		}

		// Render vertical divider
		dividerHeight := max(1, node.height)
		divider := strings.Repeat(DividerVertical+"\n", dividerHeight-1) + DividerVertical
		dividerStyled := lipgloss.NewStyle().
			Foreground(ColorBorder).
			Render(divider)

		return lipgloss.JoinHorizontal(lipgloss.Top, leftView, dividerStyled, rightView)
	}

	_, _, dividerH := splitAxis(node.height, node.SplitRatio)
	if dividerH == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, leftView, rightView)
	}

	// Vertical split - render horizontal divider
	dividerWidth := max(1, node.width)
	divider := strings.Repeat(DividerHorizontal, dividerWidth)
	dividerStyled := lipgloss.NewStyle().
		Foreground(ColorBorder).
		Render(divider)

	return lipgloss.JoinVertical(lipgloss.Left, leftView, dividerStyled, rightView)
}

// Helper function
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
