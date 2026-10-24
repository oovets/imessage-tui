package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// maxTabs is how many tabs may be open at once: alt+1 … alt+9 reach each.
const maxTabs = 9

// ErrTooManyTabs is why a new tab is refused.
var ErrTooManyTabs = fmt.Errorf("max %d tabs (alt+w to close one)", maxTabs)

// paneTab is one tab: a name and, while the tab is in the background, its
// layout — its own tree of up to maxPanes panes and which of them has focus.
//
// The active tab's layout is not kept here but in WindowManager's own
// fields, so splits, focus moves, rendering and hit-testing all work on the
// active tab without a lookup. Switching tabs parks the active layout here
// and unparks the other one.
type paneTab struct {
	name          string
	root          *LayoutNode
	windows       map[WindowID]*ChatWindow
	nextID        WindowID
	focusedWindow WindowID
}

// TabInfo describes a tab for the tab bar.
type TabInfo struct {
	// Name is the name given with alt+r, or "" for none.
	Name   string
	Active bool
	// Unseen is set when a pane in the tab holds a message not yet seen.
	Unseen bool
	// ChatGUIDs are the chats on show in the tab, in pane order, for a
	// title derived from what the tab holds.
	ChatGUIDs []string
}

// TabCount returns how many tabs are open; there is always at least one.
func (wm *WindowManager) TabCount() int {
	return len(wm.tabs)
}

// ActiveTab returns the index of the tab on screen.
func (wm *WindowManager) ActiveTab() int {
	return wm.activeTab
}

// tabWindows returns tab i's windows wherever they currently live.
func (wm *WindowManager) tabWindows(i int) map[WindowID]*ChatWindow {
	if i == wm.activeTab {
		return wm.windows
	}
	return wm.tabs[i].windows
}

// WindowsInAllTabs returns every pane in every tab.
func (wm *WindowManager) WindowsInAllTabs() []*ChatWindow {
	result := make([]*ChatWindow, 0, len(wm.windows))
	for i := range wm.tabs {
		for _, w := range wm.tabWindows(i) {
			result = append(result, w)
		}
	}
	return result
}

// Tabs describes every tab, in order.
func (wm *WindowManager) Tabs() []TabInfo {
	infos := make([]TabInfo, len(wm.tabs))
	for i, tab := range wm.tabs {
		windows := wm.tabWindows(i)
		ids := make([]WindowID, 0, len(windows))
		for id := range windows {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		info := TabInfo{Name: tab.name, Active: i == wm.activeTab}
		for _, id := range ids {
			w := windows[id]
			if w.Chat != nil {
				info.ChatGUIDs = append(info.ChatGUIDs, w.Chat.GUID)
			}
			if w.Messages.HasUnseenIncoming() {
				info.Unseen = true
			}
		}
		infos[i] = info
	}
	return infos
}

// SetActiveTabName names the active tab. An empty name goes back to the
// title derived from the tab's chats.
func (wm *WindowManager) SetActiveTabName(name string) {
	wm.tabs[wm.activeTab].name = strings.TrimSpace(name)
}

// ActiveTabName returns the name given to the active tab, or "".
func (wm *WindowManager) ActiveTabName() string {
	return wm.tabs[wm.activeTab].name
}

// NewTab opens a tab with one empty pane after the last one and switches to
// it.
func (wm *WindowManager) NewTab() error {
	if len(wm.tabs) >= maxTabs {
		return ErrTooManyTabs
	}
	window := wm.newWindow(0)
	tab := &paneTab{
		root:    NewLeafNode(window),
		windows: map[WindowID]*ChatWindow{0: window},
		nextID:  1,
	}
	wasTyping := wm.parkActive()
	wm.tabs = append(wm.tabs, tab)
	wm.unpark(len(wm.tabs)-1, wasTyping)
	return nil
}

// SwitchTab puts tab i on screen. It reports whether anything changed.
func (wm *WindowManager) SwitchTab(i int) bool {
	if i < 0 || i >= len(wm.tabs) || i == wm.activeTab {
		return false
	}
	wasTyping := wm.parkActive()
	wm.unpark(i, wasTyping)
	return true
}

// CycleTab moves step tabs along, wrapping at either end.
func (wm *WindowManager) CycleTab(step int) bool {
	n := len(wm.tabs)
	if n <= 1 {
		return false
	}
	return wm.SwitchTab(((wm.activeTab+step)%n + n) % n)
}

// CloseTab closes the active tab and its panes, and shows the tab that took
// its place. The last tab cannot be closed. The chats themselves are
// untouched: closing a tab only forgets where they were laid out.
func (wm *WindowManager) CloseTab() bool {
	if len(wm.tabs) <= 1 {
		return false
	}
	wasTyping := false
	if w, ok := wm.windows[wm.focusedWindow]; ok {
		wasTyping = w.Input.Focused()
	}
	closing := wm.activeTab
	wm.tabs = append(wm.tabs[:closing], wm.tabs[closing+1:]...)
	next := min(closing, len(wm.tabs)-1)
	wm.unpark(next, wasTyping)
	return true
}

// parkActive moves the active layout into its tab and reports whether its
// composer had focus, so the next tab can pick that up.
//
// The focused pane is unmarked on the way out: a pane in a tab that is not on
// screen is not being looked at, so a message arriving there must still mark
// it unseen.
func (wm *WindowManager) parkActive() bool {
	wasTyping := false
	if w, ok := wm.windows[wm.focusedWindow]; ok {
		wasTyping = w.Input.Focused()
		w.Focused = false
		w.Input.Blur()
	}
	tab := wm.tabs[wm.activeTab]
	tab.root = wm.root
	tab.windows = wm.windows
	tab.nextID = wm.nextID
	tab.focusedWindow = wm.focusedWindow
	wm.root = nil
	wm.windows = nil
	return wasTyping
}

// unpark makes tab i the active one, moving its layout into the manager.
func (wm *WindowManager) unpark(i int, focusInput bool) {
	tab := wm.tabs[i]
	wm.activeTab = i
	wm.root = tab.root
	wm.windows = tab.windows
	wm.nextID = tab.nextID
	wm.focusedWindow = tab.focusedWindow
	tab.root = nil
	tab.windows = nil
	// Sizes, and whether dividers take room, may have changed while the tab
	// was away.
	wm.recalculateLayout()
	if w, ok := wm.windows[wm.focusedWindow]; ok {
		w.Focused = true
		if focusInput {
			w.Input.Focus()
		}
		w.Messages.ClearIncomingUnseen()
	}
}

// restoredTab is one tab rebuilt from saved state.
type restoredTab struct {
	name    string
	root    *LayoutNode
	windows map[WindowID]*ChatWindow
	focused WindowID
}

// setTabs replaces every tab with restored ones and puts tab active on
// screen. Every window must arrive unfocused.
func (wm *WindowManager) setTabs(restored []restoredTab, active int) {
	if len(restored) == 0 {
		return
	}
	if active < 0 || active >= len(restored) {
		active = 0
	}
	wm.tabs = make([]*paneTab, len(restored))
	for i, r := range restored {
		var next WindowID
		for id := range r.windows {
			next = max(next, id+1)
		}
		wm.tabs[i] = &paneTab{
			name:          r.name,
			root:          r.root,
			windows:       r.windows,
			nextID:        next,
			focusedWindow: r.focused,
		}
	}
	wm.unpark(active, false)
}

// tabTitle is what the bar calls a tab: the name it was given, or else the
// networks its panes show, so a tab of Slack channels reads "Slack" without
// being named. label maps a chat GUID to its network's label.
func tabTitle(info TabInfo, label func(string) string) string {
	if info.Name != "" {
		return info.Name
	}
	return strings.Join(tabNetworks(info, label), "+")
}

// tabNetworks returns the networks a tab's panes show, each once, in pane
// order. label maps a chat GUID to its network's label.
func tabNetworks(info TabInfo, label func(string) string) []string {
	var labels []string
	seen := make(map[string]bool)
	for _, guid := range info.ChatGUIDs {
		l := strings.TrimSpace(label(guid))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		labels = append(labels, l)
	}
	return labels
}

// tabBarSpan is where one tab is drawn on the bar, in columns.
type tabBarSpan struct {
	tab    int
	x0, x1 int
}

// tabBarLabels lays out the tab bar: a number per tab — what alt+N goes to —
// then its title and a dot for unseen messages. Titles are cut evenly when
// the bar is too narrow for them; the numbers always stay.
func tabBarLabels(infos []TabInfo, titles []string, width int) ([]string, []tabBarSpan) {
	texts := make([]string, len(infos))
	build := func(titleWidth int) int {
		total := 0
		for i, info := range infos {
			text := " " + itoa(i+1)
			if title := titles[i]; title != "" && titleWidth > 0 {
				text += " " + truncateToWidth(title, titleWidth)
			}
			if info.Unseen {
				text += " ●"
			}
			text += " "
			texts[i] = text
			total += lipgloss.Width(text)
		}
		return total
	}
	titleWidth := 0
	for _, t := range titles {
		titleWidth = max(titleWidth, lipgloss.Width(t))
	}
	for build(titleWidth) > width && titleWidth > 0 {
		titleWidth--
	}
	spans := make([]tabBarSpan, len(infos))
	x := 0
	for i, text := range texts {
		w := lipgloss.Width(text)
		spans[i] = tabBarSpan{tab: i, x0: x, x1: x + w}
		x += w
	}
	return texts, spans
}

// renderTabBar draws the tab bar exactly width columns wide.
func renderTabBar(infos []TabInfo, titles []string, width int) string {
	texts, _ := tabBarLabels(infos, titles, width)
	active := lipgloss.NewStyle().
		Foreground(ColorChatListSelectedForeground).
		Background(ColorChatListSelectedBackground).
		Bold(true)
	unseen := lipgloss.NewStyle().Foreground(ColorChatListNewMessage)
	idle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	for i, text := range texts {
		style := idle
		switch {
		case infos[i].Active:
			style = active
		case infos[i].Unseen:
			style = unseen
		}
		b.WriteString(style.Render(text))
	}
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Height(1).
		MaxHeight(1).
		Render(truncateToWidth(b.String(), width))
}
