package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// testNetworkOf stands in for the registry: the list must never parse a GUID
// itself, so the test hands it the answer the same way the app does.
func testNetworkOf(guid string) string {
	if strings.HasPrefix(guid, "sl:") {
		return "Slack"
	}
	return "iMessage"
}

// The backends hand chats over one after another — iMessage, then Slack — so
// a mixed list is the case worth testing, not a pre-grouped one.
func networkTestList(height int) SimpleListModel {
	m := NewSimpleListModel()
	m.SetShowPreview(false)
	m.SetSize(30, height)
	m.SetNetworks(testNetworkOf, []string{"iMessage", "Slack"})
	m.SetItems([]models.Chat{
		{GUID: "sl:acme:D1", DisplayName: "Slack Anna"},
		{GUID: "chat-a", DisplayName: "Family"},
		{GUID: "sl:acme:C1", DisplayName: "General"},
		{GUID: "chat-b", DisplayName: "Bob"},
	})
	return m
}

func displayOrder(m SimpleListModel) []string {
	var names []string
	for _, idx := range m.filteredIndices {
		names = append(names, m.items[idx].DisplayName)
	}
	return names
}

// listBody is the rendered rows, without the title and network tabs.
func listBody(m SimpleListModel) []string {
	lines := strings.Split(strings.TrimRight(stripANSI(m.View()), "\n"), "\n")
	return lines[1+m.tabLineCount():]
}

func TestGroupByNetworkPutsAHeaderAboveEachNetwork(t *testing.T) {
	m := networkTestList(20)
	m.SetGroupByNetwork(true)

	want := []string{" iMessage", " Family", " Bob", " Slack", " Slack Anna", " General"}
	got := listBody(m)
	if len(got) != len(want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	for i := range want {
		if strings.TrimRight(got[i], " ") != want[i] {
			t.Fatalf("rows = %q, want %q", got, want)
		}
	}

	m.SetGroupByNetwork(false)
	if got := strings.Join(displayOrder(m), ","); got != "Slack Anna,Family,General,Bob" {
		t.Fatalf("ungrouped order = %s, want the list's own order back", got)
	}
}

// A single network has nothing to tell apart, so a header would only cost a
// row.
func TestGroupByNetworkWithOneNetworkHasNoHeader(t *testing.T) {
	m := NewSimpleListModel()
	m.SetShowPreview(false)
	m.SetSize(30, 10)
	m.SetNetworks(testNetworkOf, []string{"iMessage", "Slack"})
	m.SetItems([]models.Chat{{GUID: "chat-a", DisplayName: "Family"}})
	m.SetGroupByNetwork(true)

	if got := listBody(m); len(got) != 1 || strings.Contains(got[0], "iMessage") {
		t.Fatalf("rows = %q, want just the chat", got)
	}
	if m.tabLineCount() != 0 {
		t.Fatal("network tabs shown with only one network to choose")
	}
}

func TestCycleNetworkStepsThroughAllThenEachNetwork(t *testing.T) {
	cases := []struct {
		step int
		want []string
	}{
		{step: 1, want: []string{"iMessage", "Slack", ""}},
		{step: -1, want: []string{"Slack", "iMessage", ""}},
	}
	for _, tc := range cases {
		m := networkTestList(20)
		for i, want := range tc.want {
			got, ok := m.CycleNetwork(tc.step)
			if !ok || got != want {
				t.Fatalf("step %d, press %d: network = %q (ok %v), want %q", tc.step, i+1, got, ok, want)
			}
		}
	}
}

func TestNetworkFilterShowsOnlyThatNetworkAndCountsHiddenUnread(t *testing.T) {
	m := networkTestList(20)
	m.MarkNewMessage("sl:acme:C1")
	m.SetNetwork("iMessage")

	if got := strings.Join(displayOrder(m), ","); got != "Family,Bob" {
		t.Fatalf("filtered chats = %s, want Family,Bob", got)
	}
	// The Slack tab says something is waiting there; the tab being shown
	// needs no count.
	if got := stripANSI(strings.Join(m.tabLines(), "\n")); !strings.Contains(got, " Slack 1 ") || !strings.Contains(got, " iMessage ") {
		t.Fatalf("tabs = %q, want the Slack tab to carry its unread chat", got)
	}

	// The search box narrows within the filter, not around it.
	m.StartSearch()
	for _, r := range "bo" {
		m.AppendSearch(r)
	}
	if got := strings.Join(displayOrder(m), ","); got != "Bob" {
		t.Fatalf("search inside filter = %s, want Bob", got)
	}
}

// A filter remembered from a run with a backend that is now gone must not
// leave the list empty.
func TestNetworkFilterForMissingNetworkShowsEverything(t *testing.T) {
	m := networkTestList(20)
	m.SetNetwork("Slack · gone")

	if got := len(m.filteredIndices); got != 4 {
		t.Fatalf("visible chats = %d, want all 4", got)
	}
	if m.Network() != "Slack · gone" {
		t.Fatal("the remembered filter was dropped; it should survive until changed")
	}
}

func TestNetworkChangesKeepTheCursorOnTheSameChat(t *testing.T) {
	m := networkTestList(20)
	m.SelectByGUID("chat-b")

	m.SetGroupByNetwork(true)
	if got := m.SelectedItem().GUID; got != "chat-b" {
		t.Fatalf("after grouping selected = %s, want chat-b", got)
	}
	m.SetNetwork("iMessage")
	if got := m.SelectedItem().GUID; got != "chat-b" {
		t.Fatalf("after filtering selected = %s, want chat-b", got)
	}
	m.SetNetwork("Slack")
	if got := m.SelectedItem().GUID; got != "sl:acme:D1" {
		t.Fatalf("filtered away from the selection, selected = %s, want the first Slack chat", got)
	}
}

// Grouped, a new message lifts a chat to the top of its own group — it must
// not jump above another network's header.
func TestNewMessageTopsItsOwnGroup(t *testing.T) {
	m := networkTestList(20)
	m.SetGroupByNetwork(true)
	m.SelectByGUID("chat-a")

	m.MarkNewMessage("sl:acme:C1")

	if got := strings.Join(displayOrder(m), ","); got != "Family,Bob,General,Slack Anna" {
		t.Fatalf("grouped order = %s", got)
	}
	if got := m.SelectedItem().GUID; got != "chat-a" {
		t.Fatalf("selected = %s, want the cursor left on chat-a", got)
	}
}

// Rows from the top: title, tabs, [iMessage], Family, Bob, [Slack], Slack Anna.
func TestClickOnANetworkHeaderFiltersToIt(t *testing.T) {
	m := networkTestList(20)
	m.SetGroupByNetwork(true)

	if m.ClickAt(3, 6) {
		t.Fatal("clicking a chat reported a filter change")
	}
	if got := m.SelectedItem().GUID; got != "sl:acme:D1" {
		t.Fatalf("click on Slack Anna selected %s", got)
	}
	if !m.ClickAt(3, 5) || m.Network() != "Slack" {
		t.Fatalf("click on the Slack header: network = %q, want Slack", m.Network())
	}
}

func TestClickOnPinnedHeaderDoesNothing(t *testing.T) {
	m := networkTestList(20)
	m.TogglePin("chat-b")
	m.SelectByGUID("chat-a")

	if m.ClickAt(3, 2) || m.Network() != "" {
		t.Fatal("the Pinned header changed the filter")
	}
	if got := m.SelectedItem().GUID; got != "chat-a" {
		t.Fatalf("the Pinned header moved the cursor to %s", got)
	}
}

// Clicks are matched against the layout that was drawn, so finding a label in
// the rendered tab line and clicking on it is the real test of agreement.
func clickTab(t *testing.T, m *SimpleListModel, label string) bool {
	t.Helper()
	for i, line := range m.tabLines() {
		if col := strings.Index(stripANSI(line), " "+label+" "); col >= 0 {
			return m.ClickAt(lipgloss.Width(stripANSI(line)[:col])+1, 1+i)
		}
	}
	t.Fatalf("no %q tab in %q", label, m.tabLines())
	return false
}

func TestClickOnANetworkTabFiltersTheList(t *testing.T) {
	m := networkTestList(20)

	if !clickTab(t, &m, "Slack") || m.Network() != "Slack" {
		t.Fatalf("network = %q, want Slack", m.Network())
	}
	if got := strings.Join(displayOrder(m), ","); got != "Slack Anna,General" {
		t.Fatalf("shown = %s, want only Slack chats", got)
	}
	if clickTab(t, &m, "Slack") {
		t.Fatal("clicking the tab already shown reported a change")
	}
	if !clickTab(t, &m, "All") || m.Network() != "" {
		t.Fatalf("network = %q, want all", m.Network())
	}
}

// A narrow sidebar cannot hold every tab on one line; they wrap rather than
// widen the pane, and a wrapped tab is still clickable.
func TestNetworkTabsWrapInANarrowList(t *testing.T) {
	m := networkTestList(20)
	m.SetSize(14, 20)

	lines := m.tabLines()
	if len(lines) < 2 {
		t.Fatalf("tabs = %q, want them wrapped at width 14", lines)
	}
	for _, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w >= 14 {
			t.Fatalf("line is %d wide in a 14-wide list: %q", w, stripANSI(line))
		}
	}
	if !clickTab(t, &m, "Slack") || m.Network() != "Slack" {
		t.Fatalf("network = %q after clicking the wrapped Slack tab", m.Network())
	}
}

// Every click in the chat list used to land on the chat above the one under
// the pointer: the app passed a row index one too small.
func TestClickingAChatSelectsThatChat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := NewAppModelWithConfig(nil, nil, nil)
	app.width, app.height = 80, 12
	app.showChatList = true
	app.chatList.SetShowPreview(false)
	app.chatList.SetChats([]models.Chat{
		{GUID: "a", DisplayName: "Alpha"},
		{GUID: "b", DisplayName: "Beta"},
		{GUID: "c", DisplayName: "Gamma"},
	})
	app.updateLayout()

	for _, name := range []string{"Gamma", "Beta"} {
		row := -1
		for i, line := range strings.Split(stripANSI(app.View()), "\n") {
			if strings.Contains(line, name) {
				row = i
			}
		}
		model, _ := app.Update(tea.MouseMsg{X: 4, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		app = model.(AppModel)
		if got := app.chatList.SelectedChat().DisplayName; got != name {
			t.Fatalf("clicked %s on row %d, selected %s", name, row, got)
		}
	}
}

// Headers take rows, so scrolling must count lines rather than chats: the
// cursor stays on screen, the view stays inside the pane, and moving back up
// to a group's first chat brings its header along.
func TestGroupedScrollingCountsHeaderRows(t *testing.T) {
	const height = 5 // title, tabs, three rows
	m := networkTestList(height)
	m.SetGroupByNetwork(true)

	for i := 0; i < 3; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if got := m.SelectedItem().DisplayName; got != "General" {
		t.Fatalf("selected = %s, want General", got)
	}
	view := stripANSI(m.View())
	if got := len(strings.Split(strings.TrimRight(view, "\n"), "\n")); got > height {
		t.Fatalf("view is %d lines, pane is %d: %q", got, height, view)
	}
	if !strings.Contains(view, "General") {
		t.Fatalf("cursor scrolled out of view: %q", view)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if view := stripANSI(m.View()); !strings.Contains(view, " Slack\n") {
		t.Fatalf("first chat of the group is selected but its header is hidden: %q", view)
	}
}

func TestNetworkKeysInTheChatList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	imessage := &recordingProvider{id: "imessage"}
	slack := &recordingProvider{id: "slack:acme"}
	reg := provider.NewRegistry(imessage)
	reg.Register("sl:acme:", slack)

	app := NewAppModelWithConfig(reg, nil, nil)
	app.focused = focusChatList
	app.chatList.SetChats([]models.Chat{
		{GUID: "chat-a", DisplayName: "Family"},
		{GUID: "sl:acme:C1", DisplayName: "General"},
	})

	app = typeRunes(app, "n")
	if got := app.chatList.Network(); got != "imessage" {
		t.Fatalf("after n network = %q, want imessage", got)
	}
	app = typeRunes(app, "N")
	if got := app.chatList.Network(); got != "" {
		t.Fatalf("after N network = %q, want all", got)
	}

	app = typeRunes(app, "s")
	if !app.chatList.GroupByNetwork() {
		t.Fatal("s did not group the list by network")
	}
	if got := chatListSort(app.chatList.GroupByNetwork()); got != config.ChatListSortNetwork {
		t.Fatalf("saved sort = %q, want %q", got, config.ChatListSortNetwork)
	}

	// While searching, the letters belong to the query.
	app.chatList.StartSearch()
	app = typeRunes(app, "ns")
	if app.chatList.Network() != "" || !app.chatList.GroupByNetwork() {
		t.Fatal("n/s acted as commands while typing in the search box")
	}
}

func sectionedRows(m SimpleListModel) string {
	var parts []string
	for _, row := range m.rows {
		if row.item < 0 {
			parts = append(parts, "["+row.header+"]")
			continue
		}
		parts = append(parts, m.items[m.filteredIndices[row.item]].DisplayName)
	}
	return strings.Join(parts, ",")
}

func TestPinnedSectionLeadsTheList(t *testing.T) {
	cases := []struct {
		name    string
		grouped bool
		network string
		want    string
	}{
		{
			name: "recent",
			want: "[Pinned],Bob,Slack Anna,[Recent],Family,General",
		},
		{
			name:    "grouped",
			grouped: true,
			want:    "[Pinned],Bob,Slack Anna,[iMessage],Family,[Slack],General",
		},
		{
			// The filter reaches into Pinned too: a Slack favourite is not
			// an iMessage chat.
			name:    "filtered",
			network: "iMessage",
			want:    "[Pinned],Bob,[iMessage],Family",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := networkTestList(20)
			// Pin order, not list order, decides the Pinned section.
			m.TogglePin("chat-b")
			m.TogglePin("sl:acme:D1")
			m.SetGroupByNetwork(tc.grouped)
			m.SetNetwork(tc.network)

			if got := sectionedRows(m); got != tc.want {
				t.Fatalf("rows = %s, want %s", got, tc.want)
			}
		})
	}
}

// Without pins and without grouping there is one section, and a lone header
// would only cost a row.
func TestUnpinningLastChatDropsTheHeaders(t *testing.T) {
	m := networkTestList(20)
	m.TogglePin("chat-b")
	if m.TogglePin("chat-b") {
		t.Fatal("second toggle left the chat pinned")
	}
	if got := sectionedRows(m); strings.Contains(got, "[") {
		t.Fatalf("rows = %s, want no headers", got)
	}
}

// Favourites stay put: a message to a pinned chat does not reorder the pins,
// and a message elsewhere does not climb above them.
func TestNewMessagesDoNotReorderPinnedChats(t *testing.T) {
	m := networkTestList(20)
	m.TogglePin("chat-b")
	m.TogglePin("chat-a")

	m.MarkNewMessage("chat-a")
	m.MarkNewMessage("sl:acme:C1")

	if got, want := sectionedRows(m), "[Pinned],Bob,Family,[Recent],General,Slack Anna"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
}

func TestPinningKeepsTheCursorOnTheChat(t *testing.T) {
	m := networkTestList(20)
	m.SelectByGUID("chat-b")

	m.TogglePin("chat-b")
	if got := m.SelectedItem().GUID; got != "chat-b" {
		t.Fatalf("after pin selected = %s, want chat-b", got)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0: the pinned chat is first", m.cursor)
	}
}

func TestPinKeyPersistsAcrossRestarts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chats := []models.Chat{
		{GUID: "chat-a", DisplayName: "Family"},
		{GUID: "chat-b", DisplayName: "Bob"},
	}
	app := NewAppModelWithConfig(nil, nil, nil)
	app.focused = focusChatList
	app.chatList.SetChats(chats)
	app.chatList.SelectChatByGUID("chat-b")

	app = typeRunes(app, "p")

	restarted := NewAppModelWithConfig(nil, nil, nil)
	restarted.chatList.SetChats(chats)
	if got := restarted.chatList.SelectedChat(); got == nil || got.GUID != "chat-b" {
		t.Fatalf("after restart first chat = %v, want the pinned chat-b", got)
	}

	app = typeRunes(app, "p")
	if got := config.LoadChatOverrides().Pinned; len(got) != 0 {
		t.Fatalf("after unpin saved pins = %v, want none", got)
	}
}

func TestMovePinReordersThePinnedSection(t *testing.T) {
	const pins = "chat-b,chat-a,sl:acme:D1"
	cases := []struct {
		name    string
		pins    string
		guid    string
		step    int
		network string
		moved   bool
		want    string
	}{
		{name: "up", pins: pins, guid: "sl:acme:D1", step: -1, moved: true, want: "chat-b,sl:acme:D1,chat-a"},
		{name: "down", pins: pins, guid: "chat-b", step: 1, moved: true, want: "chat-a,chat-b,sl:acme:D1"},
		{name: "top edge", pins: pins, guid: "chat-b", step: -1, want: pins},
		{name: "bottom edge", pins: pins, guid: "sl:acme:D1", step: 1, want: pins},
		{name: "unpinned", pins: pins, guid: "sl:acme:C1", step: -1, want: pins},
		{
			// The Slack pin between them is filtered out, so the move
			// trades with the next pin in view instead of doing nothing.
			name: "skips hidden pin", pins: "chat-b,sl:acme:D1,chat-a",
			guid: "chat-b", step: 1, network: "iMessage",
			moved: true, want: "chat-a,sl:acme:D1,chat-b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := networkTestList(20)
			m.SetPinned(strings.Split(tc.pins, ","))
			m.SetNetwork(tc.network)

			if got := m.MovePin(tc.guid, tc.step); got != tc.moved {
				t.Fatalf("moved = %v, want %v", got, tc.moved)
			}
			if got := strings.Join(m.Pinned(), ","); got != tc.want {
				t.Fatalf("pins = %s, want %s", got, tc.want)
			}
			if tc.moved && m.SelectedItem().GUID != tc.guid {
				t.Fatalf("cursor on %s, want it to follow %s", m.SelectedItem().GUID, tc.guid)
			}
		})
	}
}

func TestPinKeysMoveAndSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := NewAppModelWithConfig(nil, nil, nil)
	app.focused = focusChatList
	app.chatList.SetChats([]models.Chat{
		{GUID: "chat-a", DisplayName: "Family"},
		{GUID: "chat-b", DisplayName: "Bob"},
		{GUID: "chat-c", DisplayName: "Cara"},
	})
	app.chatList.SetPinned([]string{"chat-a", "chat-b"})
	app.chatList.SelectChatByGUID("chat-b")

	app = typeRunes(app, "K")
	if got := strings.Join(config.LoadChatOverrides().Pinned, ","); got != "chat-b,chat-a" {
		t.Fatalf("saved pins after K = %s, want chat-b,chat-a", got)
	}
	app = typeRunes(app, "J")
	if got := strings.Join(app.chatList.Pinned(), ","); got != "chat-a,chat-b" {
		t.Fatalf("pins after J = %s, want chat-a,chat-b", got)
	}

	// An unpinned chat has nowhere to move to; K must not start pinning it.
	app.chatList.SelectChatByGUID("chat-c")
	app = typeRunes(app, "K")
	if app.chatList.IsPinned("chat-c") {
		t.Fatal("K pinned an unpinned chat")
	}
}

// ctrl+o pins from a pane, and as a ctrl key it must keep working while the
// composer has text in it — without typing anything into it.
func TestCtrlOPinsThePaneChatWhileComposing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := typeNewComposer(t, "hej")

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	app = model.(AppModel)

	if !app.chatList.IsPinned("chat-a") {
		t.Fatal("ctrl+o did not pin the pane's chat")
	}
	if got := app.windowManager.FocusedWindow().Input.GetText(); got != "hej" {
		t.Fatalf("composer = %q, want it untouched", got)
	}
}

// An ignored chat is gone for good: not listed, not counted, and a new message
// does not bring it back.
func TestIgnoredChatStaysOutOfTheList(t *testing.T) {
	m := networkTestList(20)
	m.TogglePin("sl:acme:C1")

	if !m.ToggleIgnore("sl:acme:C1") {
		t.Fatal("ToggleIgnore did not report the chat as ignored")
	}
	m.MarkNewMessage("sl:acme:C1")

	if got := sectionedRows(m); strings.Contains(got, "General") {
		t.Fatalf("rows = %s, want the ignored chat left out", got)
	}
	if m.IsPinned("sl:acme:C1") {
		t.Fatal("ignoring left the chat pinned")
	}
	if got := m.NewMessageCount(); got != 0 {
		t.Fatalf("NewMessageCount = %d, want the ignored chat not counted", got)
	}
	if tabs := stripANSI(strings.Join(m.tabLines(), " ")); strings.Contains(tabs, "Slack 1") {
		t.Fatalf("tabs = %q, want no unread count for an ignored chat", tabs)
	}
}

// Ignoring the selected chat takes it out from under the cursor; the cursor
// should land on what took its place, not jump to the top.
func TestIgnoringKeepsTheCursorsPlace(t *testing.T) {
	m := networkTestList(20)
	m.SelectByGUID("sl:acme:C1") // third of four

	m.ToggleIgnore("sl:acme:C1")

	if got := m.SelectedItem().GUID; got != "chat-b" {
		t.Fatalf("selected = %s, want chat-b, the chat that moved into its row", got)
	}
}

func TestShowIgnoredListsThemLastAndCanBringOneBack(t *testing.T) {
	m := networkTestList(20)
	m.ToggleIgnore("chat-a")

	m.SetShowIgnored(true)
	if got, want := sectionedRows(m), "[Recent],Slack Anna,General,Bob,[Ignored],Family"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}

	m.SelectByGUID("chat-a")
	if m.ToggleIgnore("chat-a") {
		t.Fatal("second toggle left the chat ignored")
	}
	if got := m.SelectedItem().GUID; got != "chat-a" {
		t.Fatalf("selected = %s, want the cursor to follow the chat back", got)
	}
	m.SetShowIgnored(false)
	if got := sectionedRows(m); strings.Contains(got, "[") {
		t.Fatalf("rows = %s, want a plain list again", got)
	}
}

// Ignore every chat of a network and there is nothing left to filter to, so
// its tab — and with one network left, the whole tab row — goes.
func TestIgnoringAWholeNetworkDropsTheTabs(t *testing.T) {
	m := networkTestList(20)
	m.ToggleIgnore("sl:acme:D1")
	m.ToggleIgnore("sl:acme:C1")

	if got := m.tabLineCount(); got != 0 {
		t.Fatalf("tab rows = %d, want none with only iMessage left", got)
	}
}

func TestIgnoreKeysPersistAcrossRestarts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chats := []models.Chat{
		{GUID: "chat-a", DisplayName: "Family"},
		{GUID: "chat-b", DisplayName: "Bob"},
	}
	app := NewAppModelWithConfig(nil, nil, nil)
	app.focused = focusChatList
	app.chatList.SetChats(chats)
	app.chatList.SelectChatByGUID("chat-a")

	app = typeRunes(app, "x")

	restarted := NewAppModelWithConfig(nil, nil, nil)
	restarted.focused = focusChatList
	restarted.chatList.SetChats(chats)
	if got := restarted.chatList.SelectedChat(); got == nil || got.GUID != "chat-b" {
		t.Fatalf("after restart first chat = %v, want chat-a still ignored", got)
	}

	restarted = typeRunes(restarted, "I")
	if !restarted.chatList.ShowIgnored() {
		t.Fatal("I did not show ignored chats")
	}
	restarted.chatList.SelectChatByGUID("chat-a")
	restarted = typeRunes(restarted, "x")
	if got := config.LoadChatOverrides().Ignored; len(got) != 0 {
		t.Fatalf("saved ignores = %v, want none after x on the ignored chat", got)
	}
}

func unknownSenderList() SimpleListModel {
	m := NewSimpleListModel()
	m.SetShowPreview(false)
	m.SetSize(30, 20)
	m.SetItems([]models.Chat{
		{GUID: "code", DisplayName: "Code", UnknownSender: true},
		{GUID: "nora", DisplayName: "Nora"},
		{GUID: "bank", DisplayName: "Bank", UnknownSender: true},
		{GUID: "mormor", DisplayName: "Mormor"},
	})
	return m
}

func TestUnknownSendersHiddenUntilShown(t *testing.T) {
	m := unknownSenderList()
	if got := sectionedRows(m); got != "Nora,Mormor" {
		t.Fatalf("rows = %s, want only known senders", got)
	}

	m.SetShowUnknown(true)
	if got, want := sectionedRows(m), "[Recent],Nora,Mormor,[Unknown senders],Code,Bank"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
	if got := m.UnknownCount(); got != 2 {
		t.Fatalf("UnknownCount = %d, want 2", got)
	}
}

// A one-time code is the reason to look at an unknown sender at all, so a new
// message lists the chat at the top even with the section hidden — also when
// the chat was on top of the list already, which used to skip the rebuild.
func TestUnknownSenderWithNewMessageIsListed(t *testing.T) {
	m := unknownSenderList()

	m.MarkNewMessage("code")
	if got := sectionedRows(m); got != "Code,Nora,Mormor" {
		t.Fatalf("rows = %s, want the new code listed on top", got)
	}
	if got := m.NewMessageCount(); got != 1 {
		t.Fatalf("NewMessageCount = %d, want 1", got)
	}
}

func TestPinnedUnknownSenderStaysInView(t *testing.T) {
	m := unknownSenderList()
	m.SetShowUnknown(true)
	m.TogglePin("bank")
	m.SetShowUnknown(false)

	if got := sectionedRows(m); got != "[Pinned],Bank,[Recent],Nora,Mormor" {
		t.Fatalf("rows = %s, want the pinned unknown sender kept", got)
	}
}

func TestChatListTimeCanBeHidden(t *testing.T) {
	m := NewSimpleListModel()
	m.SetShowPreview(false)
	m.SetSize(30, 8)
	m.SetItems([]models.Chat{{GUID: "a", DisplayName: "Alpha", LastMessageTime: 1000}})
	stamp := formatChatListTime(1000)

	if !strings.Contains(stripANSI(m.View()), stamp) {
		t.Fatalf("time %q not shown by default: %q", stamp, stripANSI(m.View()))
	}
	m.SetShowTime(false)
	if strings.Contains(stripANSI(m.View()), stamp) {
		t.Fatalf("time %q still shown: %q", stamp, stripANSI(m.View()))
	}
}

func TestTKeyTogglesChatListTimeAndRemembersIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := NewAppModelWithConfig(nil, nil, nil)
	app.focused = focusChatList
	app.chatList.SetChats([]models.Chat{{GUID: "a", DisplayName: "Alpha", LastMessageTime: 1000}})

	app = typeRunes(app, "t")
	if app.showChatListTime || app.chatList.list.showTime {
		t.Fatal("t did not hide the chat list times")
	}
	app.persist.flushAll()
	if config.LoadUIState().ShowChatListTime {
		t.Fatal("hidden chat list times were not saved")
	}

	// While searching, t is a letter of the query.
	app.chatList.StartSearch()
	app = typeRunes(app, "t")
	if app.showChatListTime {
		t.Fatal("t toggled the times while typing in the search box")
	}
}
