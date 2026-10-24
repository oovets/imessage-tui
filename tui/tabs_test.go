package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// rowContaining returns the screen row whose text contains s, or -1.
func rowContaining(view, s string) int {
	for i, line := range strings.Split(stripANSI(view), "\n") {
		if strings.Contains(line, s) {
			return i
		}
	}
	return -1
}

// newTabTestApp is an app of the given size starting from a first run: the
// package's shared HOME holds whatever other tests' debounced writes left,
// and a restored message cache turns a test's messages into duplicates.
func newTabTestApp(t *testing.T, width, height int) AppModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	app := NewAppModelWithConfig(nil, nil, nil)
	app.width, app.height = width, height
	app.updateLayout()
	return app
}

func click(t *testing.T, app AppModel, x, y int) AppModel {
	t.Helper()
	model, _ := app.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return model.(AppModel)
}

func press(t *testing.T, app AppModel, msg tea.KeyMsg) AppModel {
	t.Helper()
	model, _ := app.Update(msg)
	return model.(AppModel)
}

func altKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}
}

// stackedPanesApp is an app with two panes stacked top and bottom, the top
// one focused, each showing its own chat.
func stackedPanesApp(t *testing.T) AppModel {
	t.Helper()
	app := newTabTestApp(t, 80, 20)
	app.showChatList = false
	app.updateLayout()
	top := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(top, &models.Chat{GUID: "chat-top", DisplayName: "Upstairs"})
	if err := app.windowManager.SplitWindow(SplitVertical); err != nil {
		t.Fatal(err)
	}
	bottom := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(bottom, &models.Chat{GUID: "chat-bottom", DisplayName: "Downstairs"})
	app.windowManager.SetFocus(top.ID)
	app.focused = focusWindow
	app.updateLayout()
	return app
}

// A click on a pane's header row focuses that pane. The pane area used to be
// hit-tested one row too high, so the header of a lower pane resolved to the
// divider above it and started a drag instead.
func TestClickingAPaneHeaderFocusesThatPane(t *testing.T) {
	for _, tabs := range []int{1, 2} {
		app := stackedPanesApp(t)
		if tabs == 2 {
			// A second tab puts the bar above the panes; go back to the
			// first so the stacked panes are on screen under it.
			if err := app.windowManager.NewTab(); err != nil {
				t.Fatal(err)
			}
			app.windowManager.SwitchTab(0)
			app.updateLayout()
		}
		row := rowContaining(app.View(), "Downstairs")
		if row < 0 {
			t.Fatalf("%d tabs: bottom pane header not drawn:\n%s", tabs, stripANSI(app.View()))
		}
		app = click(t, app, 10, row)
		if got := app.windowManager.FocusedWindow().Chat.GUID; got != "chat-bottom" {
			t.Errorf("%d tabs: clicked the bottom header on row %d, focused %s", tabs, row, got)
		}
		if app.dividerDragging {
			t.Errorf("%d tabs: clicking the header started a divider drag", tabs)
		}
	}
}

func twoNetworkApp(t *testing.T) AppModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	imessage := &recordingProvider{id: "imessage"}
	slack := &recordingProvider{id: "slack:acme"}
	registry := provider.NewRegistry(imessage)
	registry.Register("sl:acme:", slack)
	app := NewAppModelWithConfig(registry, nil, nil)
	app.width, app.height = 100, 30
	app.updateLayout()
	return app
}

func TestTabsKeepTheirOwnPanes(t *testing.T) {
	wm := NewWindowManager()
	wm.SetSize(200, 60)
	if err := wm.SplitWindow(SplitHorizontal); err != nil {
		t.Fatal(err)
	}
	focused := wm.FocusedWindow()

	if err := wm.NewTab(); err != nil {
		t.Fatal(err)
	}
	if wm.TabCount() != 2 || wm.ActiveTab() != 1 || wm.WindowCount() != 1 {
		t.Fatalf("new tab: %d tabs, active %d, %d panes; want 2, 1, 1", wm.TabCount(), wm.ActiveTab(), wm.WindowCount())
	}
	// The pane limit is per tab: this one fills up on its own.
	if err := splitToMaxPanes(t, wm); !errors.Is(err, ErrTooManyPanes) {
		t.Fatalf("splitting stopped with %v", err)
	}
	if got := wm.WindowCount(); got != maxPanes {
		t.Fatalf("second tab has %d panes, want %d", got, maxPanes)
	}

	if !wm.SwitchTab(0) {
		t.Fatal("SwitchTab(0) did nothing")
	}
	if got := wm.WindowCount(); got != 2 {
		t.Errorf("first tab came back with %d panes, want 2", got)
	}
	if wm.FocusedWindow() != focused || !focused.Focused {
		t.Errorf("first tab came back with a different pane focused")
	}
	if got := len(wm.WindowsInAllTabs()); got != 2+maxPanes {
		t.Errorf("%d panes across tabs, want %d", got, 2+maxPanes)
	}
	for _, w := range wm.WindowsInAllTabs() {
		if w.Focused && w != focused {
			t.Errorf("pane %d in a background tab is still marked focused", w.ID)
		}
	}
}

func TestClosingTabs(t *testing.T) {
	wm := NewWindowManager()
	wm.SetSize(100, 30)
	for range 2 {
		if err := wm.NewTab(); err != nil {
			t.Fatal(err)
		}
	}
	wm.SetActiveTabName("third")
	wm.SwitchTab(1)
	wm.SetActiveTabName("second")

	if !wm.CloseTab() {
		t.Fatal("closing the middle tab refused")
	}
	// The tab to the right takes the closed one's place.
	if wm.TabCount() != 2 || wm.ActiveTab() != 1 || wm.ActiveTabName() != "third" {
		t.Fatalf("after close: %d tabs, active %d %q; want 2, 1 \"third\"", wm.TabCount(), wm.ActiveTab(), wm.ActiveTabName())
	}
	if !wm.CloseTab() {
		t.Fatal("closing the last of two tabs refused")
	}
	if wm.TabCount() != 1 || wm.ActiveTab() != 0 || wm.FocusedWindow() == nil {
		t.Fatalf("after second close: %d tabs, active %d", wm.TabCount(), wm.ActiveTab())
	}
	if wm.CloseTab() {
		t.Error("closed the only tab")
	}
}

func TestTabLimitAndCycling(t *testing.T) {
	wm := NewWindowManager()
	for wm.TabCount() < maxTabs {
		if err := wm.NewTab(); err != nil {
			t.Fatal(err)
		}
	}
	if err := wm.NewTab(); !errors.Is(err, ErrTooManyTabs) {
		t.Fatalf("tab %d: %v, want the tab limit", maxTabs+1, err)
	}
	if !wm.CycleTab(1) || wm.ActiveTab() != 0 {
		t.Errorf("next from the last tab went to %d, want 0", wm.ActiveTab())
	}
	if !wm.CycleTab(-1) || wm.ActiveTab() != maxTabs-1 {
		t.Errorf("previous from the first tab went to %d, want %d", wm.ActiveTab(), maxTabs-1)
	}
}

func TestTabTitle(t *testing.T) {
	label := func(guid string) string {
		switch {
		case strings.HasPrefix(guid, "sl:"):
			return "Slack"
		case guid == "":
			return ""
		}
		return "iMessage"
	}
	tests := []struct {
		name string
		info TabInfo
		want string
	}{
		{"named", TabInfo{Name: "Jobb", ChatGUIDs: []string{"sl:a:1"}}, "Jobb"},
		{"empty", TabInfo{}, ""},
		{"one network", TabInfo{ChatGUIDs: []string{"sl:a:1", "sl:a:2"}}, "Slack"},
		{"mixed, in pane order", TabInfo{ChatGUIDs: []string{"im-1", "sl:a:1", "im-2"}}, "iMessage+Slack"},
	}
	for _, tt := range tests {
		if got := tabTitle(tt.info, label); got != tt.want {
			t.Errorf("%s: tabTitle = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// A message for a chat whose only pane is in a background tab still lands in
// that pane, and marks it — and its tab — unseen until the tab is shown.
func TestBackgroundTabTakesRealtimeMessages(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	app := newTabTestApp(t, 100, 30)
	window := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(window, &models.Chat{GUID: "chat-a", DisplayName: "Family"})
	app.focused = focusWindow
	app = press(t, app, altKey('t'))

	model, _ := app.Update(providerEventMsg(provider.Event{
		Kind:     provider.EventNewMessage,
		ChatGUID: "chat-a",
		Message:  models.Message{GUID: "m1", ChatGUID: "chat-a", Text: "middag?", DateCreated: 1000},
	}))
	app = *model.(*AppModel)

	if got := len(window.Messages.messages); got != 1 {
		t.Fatalf("background pane holds %d messages, want 1", got)
	}
	if !window.Messages.HasUnseenIncoming() || !app.windowManager.Tabs()[0].Unseen {
		t.Fatal("background tab not marked unseen")
	}
	if bar := strings.Split(stripANSI(app.View()), "\n")[0]; !strings.Contains(bar, "●") {
		t.Errorf("tab bar shows no unseen marker: %q", bar)
	}

	app = press(t, app, altKey('1'))
	if app.windowManager.ActiveTab() != 0 {
		t.Fatalf("alt+1 left tab %d on screen", app.windowManager.ActiveTab())
	}
	if window.Messages.HasUnseenIncoming() || app.windowManager.Tabs()[0].Unseen {
		t.Error("showing the tab did not clear its unseen marker")
	}
}

// Tab keys are alt keys, so they work mid-message and never type into the
// composer.
func TestTabKeysWorkWhileTyping(t *testing.T) {
	app := newTabTestApp(t, 100, 30)
	first := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(first, &models.Chat{GUID: "chat-a", DisplayName: "Family"})
	app.focused = focusWindow
	first.Input.Focus()
	first.Input.textarea.SetValue("hej")

	app = press(t, app, altKey('t'))
	if app.windowManager.TabCount() != 2 || app.windowManager.ActiveTab() != 1 {
		t.Fatalf("alt+t: %d tabs, active %d", app.windowManager.TabCount(), app.windowManager.ActiveTab())
	}
	second := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(second, &models.Chat{GUID: "chat-b", DisplayName: "Jobb"})

	for _, step := range []struct {
		key  rune
		want int
	}{{',', 0}, {'.', 1}, {'1', 0}, {'2', 1}, {'9', 1}} {
		app = press(t, app, altKey(step.key))
		if got := app.windowManager.ActiveTab(); got != step.want {
			t.Errorf("alt+%c: tab %d on screen, want %d", step.key, got, step.want)
		}
	}
	app = press(t, app, altKey('1'))
	if got := app.windowManager.FocusedWindow(); got != first || !got.Input.Focused() {
		t.Fatal("back on the first tab, its composer does not have focus")
	}
	if got := first.Input.GetText(); got != "hej" {
		t.Errorf("composer reads %q after the tab keys, want %q", got, "hej")
	}

	app = press(t, app, altKey('r'))
	for _, r := range "Hem" {
		app = press(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	app = press(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if got := app.windowManager.ActiveTabName(); got != "Hem" {
		t.Errorf("alt+r named the tab %q, want %q", got, "Hem")
	}
	if got := first.Input.GetText(); got != "hej" {
		t.Errorf("naming the tab changed the composer to %q", got)
	}

	app = press(t, app, altKey('w'))
	if app.windowManager.TabCount() != 1 || app.windowManager.FocusedWindow() != second {
		t.Errorf("alt+w: %d tabs, focused pane is not the remaining tab's", app.windowManager.TabCount())
	}
}

// The bar takes a row from the panes only while there is more than one tab.
func TestTabBarTakesARowOnlyWithTabs(t *testing.T) {
	app := newTabTestApp(t, 100, 30)
	if got := app.windowManager.height; got != 30 {
		t.Fatalf("one tab: panes are %d rows, want 30", got)
	}
	app = press(t, app, altKey('t'))
	if got := app.windowManager.height; got != 29 {
		t.Errorf("two tabs: panes are %d rows, want 29", got)
	}
	app = press(t, app, altKey('w'))
	if got := app.windowManager.height; got != 30 {
		t.Errorf("back to one tab: panes are %d rows, want 30", got)
	}
}

func TestTabBarStaysInsideTheTerminal(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	// Full panes only from 80x24, the smallest size the pane limit itself is
	// held to; below that a narrow bar over one pane per tab.
	for _, tt := range []struct {
		width, height int
		fullPanes     bool
	}{{40, 10, false}, {80, 24, true}, {120, 40, true}, {200, 60, true}} {
		for _, sidebar := range []bool{true, false} {
			what := fmt.Sprintf("nine named tabs, sidebar %v, full panes %v", sidebar, tt.fullPanes)
			app := NewAppModelWithConfig(nil, nil, nil)
			app.showChatList = sidebar
			model, _ := app.Update(tea.WindowSizeMsg{Width: tt.width, Height: tt.height})
			app = model.(AppModel)
			for app.windowManager.TabCount() < maxTabs {
				app = press(t, app, altKey('t'))
				app.windowManager.SetActiveTabName("Ett väldigt långt namn 🎉 " + itoa(app.windowManager.TabCount()))
			}
			if tt.fullPanes {
				if err := splitToMaxPanes(t, app.windowManager); !errors.Is(err, ErrTooManyPanes) {
					t.Fatal(err)
				}
			}
			app.updateLayout()
			assertFrameFits(t, app, tt.width, tt.height, what)
			if bar := app.tabBarView(); lipgloss.Width(bar) != app.windowManager.width || lipgloss.Height(bar) != 1 {
				t.Errorf("%dx%d, %s: bar is %dx%d, want %dx1", tt.width, tt.height, what,
					lipgloss.Width(bar), lipgloss.Height(bar), app.windowManager.width)
			}
		}
	}
}

func TestClickingATabSwitchesToIt(t *testing.T) {
	app := newTabTestApp(t, 120, 30)
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		if name != "Alpha" {
			app = press(t, app, altKey('t'))
		}
		app.windowManager.SetActiveTabName(name)
	}
	app = press(t, app, altKey('1'))

	bar := strings.Split(stripANSI(app.View()), "\n")[0]
	col := strings.Index(bar, "Beta")
	if col < 0 {
		t.Fatalf("Beta not on the bar: %q", bar)
	}
	// The bar starts where the panes do, after the chat list; the text up to
	// "Beta" is plain ASCII, so its byte index is its column.
	app = click(t, app, col+1, 0)
	if got := app.windowManager.ActiveTab(); got != 1 {
		t.Errorf("clicked Beta at column %d, tab %d on screen", col, got)
	}
}

// Polling reloads only the tab on screen; a tab coming back is caught up if
// its chats went stale while it was hidden.
func TestOnlyTheTabOnScreenIsPolled(t *testing.T) {
	app := twoNetworkApp(t)
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &models.Chat{GUID: "im-1", DisplayName: "Family"})
	app.messageFetchedAt["im-1"] = time.Now().Add(-time.Hour)
	app.windowManager.SetCachedMessages("im-1", []models.Message{{GUID: "old", Text: "x", DateCreated: 1}})
	app = press(t, app, altKey('t'))
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &models.Chat{GUID: "sl:acme:C1", DisplayName: "#general"})

	app.refreshOpenChatsCmd()
	if !app.messageFetchInFlight["sl:acme:C1"] {
		t.Error("the chat on screen was not polled")
	}
	if app.messageFetchInFlight["im-1"] {
		t.Error("a chat in a background tab was polled")
	}

	app = press(t, app, altKey('1'))
	if !app.messageFetchInFlight["im-1"] {
		t.Error("coming back to a stale tab did not fetch its chat")
	}
}

// The chat list follows the tab on screen: a tab's one network, every
// network for a tab that mixes them, and whatever it was for an empty tab.
func TestChatListFollowsTheTabsNetwork(t *testing.T) {
	app := twoNetworkApp(t)
	app.chatList.SetChats([]models.Chat{
		{GUID: "im-1", DisplayName: "Family"},
		{GUID: "sl:acme:C1", DisplayName: "Engineering"},
	})
	show := func(guid string) {
		app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &models.Chat{GUID: guid})
	}
	show("im-1")
	app = press(t, app, altKey('t'))
	show("sl:acme:C1")
	app = press(t, app, altKey('t'))
	show("im-1")
	if err := app.windowManager.SplitWindow(SplitHorizontal); err != nil {
		t.Fatal(err)
	}
	show("sl:acme:C1")
	app = press(t, app, altKey('t'))

	steps := []struct {
		tab  rune
		want []string // chats the list shows
	}{
		{'1', []string{"Family"}},
		{'2', []string{"Engineering"}},
		{'3', []string{"Family", "Engineering"}},
		{'2', []string{"Engineering"}},
		{'4', []string{"Engineering"}},
		{'1', []string{"Family"}},
		{'4', []string{"Family"}},
	}
	for i, step := range steps {
		app = press(t, app, altKey(step.tab))
		list := stripANSI(app.chatList.View())
		for _, name := range []string{"Family", "Engineering"} {
			want := false
			for _, w := range step.want {
				want = want || w == name
			}
			if got := strings.Contains(list, name); got != want {
				t.Errorf("step %d, tab %c: %s listed = %v, want %v (filter %q)\n%s",
					i, step.tab, name, got, want, app.chatList.Network(), list)
			}
		}
	}
}

func TestDisplayTogglesReachBackgroundTabs(t *testing.T) {
	app := newTabTestApp(t, 100, 30)
	background := app.windowManager.FocusedWindow()
	app = press(t, app, altKey('t'))
	app.showTimestamps = true
	app = press(t, app, tea.KeyMsg{Type: tea.KeyCtrlT})
	if background.Messages.showTimestamps {
		t.Error("ctrl+t did not reach a pane in a background tab")
	}
}

func TestTabsSurviveARestart(t *testing.T) {
	chats := []models.Chat{
		{GUID: "im-1", DisplayName: "Family"},
		{GUID: "sl:acme:C1", DisplayName: "#general"},
		{GUID: "sl:acme:C2", DisplayName: "#random"},
	}
	app := twoNetworkApp(t)
	app.chatList.SetChats(chats)
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &chats[0])
	app = press(t, app, altKey('t'))
	app.windowManager.SetActiveTabName("Jobb")
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &chats[1])
	if err := app.windowManager.SplitWindow(SplitHorizontal); err != nil {
		t.Fatal(err)
	}
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &chats[2])

	state, ok := app.layoutState()
	if !ok {
		t.Fatal("no layout to save")
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var loaded config.LayoutState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	// The pre-tabs fields mirror the active tab, for an older build.
	if !reflect.DeepEqual(loaded.LeafChatGUIDs, []string{"sl:acme:C1", "sl:acme:C2"}) || loaded.FocusedLeafIndex != 1 {
		t.Errorf("legacy fields = %v focus %d, want the active tab", loaded.LeafChatGUIDs, loaded.FocusedLeafIndex)
	}

	restarted := twoNetworkApp(t)
	restarted.savedLayoutState = &loaded
	if !restarted.restoreLayoutFromState(chats) {
		t.Fatal("layout not restored")
	}
	wm := restarted.windowManager
	if wm.TabCount() != 2 || wm.ActiveTab() != 1 {
		t.Fatalf("restored %d tabs, active %d; want 2, 1", wm.TabCount(), wm.ActiveTab())
	}
	tabs := wm.Tabs()
	if tabs[1].Name != "Jobb" || tabs[0].Name != "" {
		t.Errorf("tab names = %q, %q", tabs[0].Name, tabs[1].Name)
	}
	if !reflect.DeepEqual(tabs[0].ChatGUIDs, []string{"im-1"}) || !reflect.DeepEqual(tabs[1].ChatGUIDs, []string{"sl:acme:C1", "sl:acme:C2"}) {
		t.Errorf("restored chats = %v / %v", tabs[0].ChatGUIDs, tabs[1].ChatGUIDs)
	}
	if got := wm.FocusedWindow().Chat.GUID; got != "sl:acme:C2" {
		t.Errorf("focused pane shows %s, want sl:acme:C2", got)
	}
	focused := 0
	for _, w := range wm.WindowsInAllTabs() {
		if w.Focused {
			focused++
		}
	}
	if focused != 1 {
		t.Errorf("%d panes marked focused across tabs, want 1", focused)
	}
	if got := restarted.tabTitles(tabs); !reflect.DeepEqual(got, []string{"imessage", "Jobb"}) {
		t.Errorf("tab titles = %q", got)
	}
}
