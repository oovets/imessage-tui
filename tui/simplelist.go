package tui

import (
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

// stripEmojis removes emoji and symbol characters from a string using an
// allowlist approach: only letters, digits, spaces, and common name punctuation
// are kept. This handles all emoji blocks without enumerating them.
func stripEmojis(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r): // keeps all scripts: Latin, Cyrillic, Arabic, CJK, etc.
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(r)
		case r == '-' || r == '\'' || r == '.' || r == ',' || r == '(' || r == ')':
			b.WriteRune(r)
			// skip everything else (emoji, symbols, variation selectors, ZWJ…)
		}
	}
	return strings.TrimSpace(b.String())
}

// listRow is one entry on screen: a chat, or the header above a network's
// group of chats when the list is grouped by network.
type listRow struct {
	header  string // set on header rows only
	network string // on a network section's header: the network it names
	item    int    // index into filteredIndices; -1 on header rows
}

// networkTab is one clickable chip in the row of network filters under the
// title. The line and x range say exactly where it was drawn, so a click is
// matched against the same layout the view rendered.
type networkTab struct {
	network string // "" is All
	text    string
	unread  int
	line    int
	x0, x1  int // x1 exclusive
}

// SimpleListModel is a simple scrollable list without auto-centering
type SimpleListModel struct {
	items []models.Chat
	// filteredIndices is the display order: the chats that pass the search and
	// network filters, reordered into groups when grouping by network. The
	// cursor indexes it, so headers are never selectable.
	filteredIndices []int
	rows            []listRow
	cursor          int
	offset          int // scroll offset (which row is at the top)
	width           int
	height          int
	focused         bool
	loading         bool
	showPreview     bool
	searchActive    bool
	searchQuery     string
	// The GUID is opaque here, so which network a chat belongs to is asked of
	// networkOf; networkOrder is the order groups appear in.
	networkOf      func(chatGUID string) string
	networkOrder   []string
	network        string // filter; "" shows every network
	groupByNetwork bool
	// pinned holds favourite chats by GUID, in the order they were pinned —
	// the order the Pinned section shows them in, so favourites stay put
	// instead of reshuffling on every message.
	pinned []string
	// ignored chats are left out of the list, the Pinned section and every
	// unread count — what the user asked never to be shown. showIgnored brings
	// them back in an Ignored section, which is how one gets un-ignored.
	ignored     []string
	ignoredSet  map[string]bool
	showIgnored bool
	// showUnknown lists every chat from an unknown sender in its own section.
	// Hidden, the only ones listed are those with a new message — a code
	// that just arrived has to be findable — and those pinned.
	showUnknown     bool
	selectedStyle   lipgloss.Style
	normalStyle     lipgloss.Style
	newMessageStyle lipgloss.Style
	previewStyle    lipgloss.Style
	timeStyle       lipgloss.Style
	searchStyle     lipgloss.Style
	headerStyle     lipgloss.Style
}

func NewSimpleListModel() SimpleListModel {
	return SimpleListModel{
		cursor:      0,
		offset:      0,
		showPreview: true,
		selectedStyle: lipgloss.NewStyle().
			Foreground(ColorChatListSelectedForeground).
			Background(ColorChatListSelectedBackground),
		normalStyle: lipgloss.NewStyle(),
		newMessageStyle: lipgloss.NewStyle().
			Foreground(ColorChatListNewMessage),
		previewStyle: lipgloss.NewStyle().
			Foreground(ColorMuted),
		timeStyle: lipgloss.NewStyle().
			Foreground(ColorMuted),
		searchStyle: lipgloss.NewStyle().
			Foreground(ColorAccent).
			Italic(true),
		headerStyle: lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true),
	}
}

func (m *SimpleListModel) SetItems(chats []models.Chat) {
	m.items = chats
	m.rebuildFilter()
	m.clampCursor()
}

func (m *SimpleListModel) RemoveByGUID(chatGUID string) bool {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.items = append(m.items[:i], m.items[i+1:]...)
		m.rebuildFilter()
		m.clampCursor()
		return true
	}
	return false
}

func (m *SimpleListModel) SetLocalDisplayName(chatGUID, displayName string) bool {
	for i := range m.items {
		if m.items[i].GUID != chatGUID {
			continue
		}
		m.items[i].LocalDisplayName = strings.TrimSpace(displayName)
		m.rebuildFilter()
		return true
	}
	return false
}

func (m *SimpleListModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *SimpleListModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m *SimpleListModel) SetLoading(loading bool) {
	m.loading = loading
}

func (m *SimpleListModel) SetShowPreview(show bool) {
	m.showPreview = show
}

// SetNetworks tells the list how to name a chat's network and in which order
// groups are shown. A nil networkOf puts every chat in one unnamed network.
func (m *SimpleListModel) SetNetworks(networkOf func(chatGUID string) string, order []string) {
	m.networkOf = networkOf
	m.networkOrder = order
	m.refreshKeepingSelection()
}

// SetNetwork filters the list to one network; "" shows them all. A network
// with no chats in the list is treated as no filter, so a remembered filter
// for a backend that is gone does not leave the list empty.
func (m *SimpleListModel) SetNetwork(network string) {
	m.network = network
	m.refreshKeepingSelection()
}

// Network is the filter as set, which may name a network not in the list.
func (m *SimpleListModel) Network() string {
	return m.network
}

// CycleNetwork steps the filter through all networks and every single one, in
// group order. ok is false when there is nothing to cycle between.
func (m *SimpleListModel) CycleNetwork(step int) (network string, ok bool) {
	var nets []string
	for _, n := range m.networks() {
		if n != "" {
			nets = append(nets, n)
		}
	}
	if len(nets) < 2 {
		return "", false
	}
	// Position 0 is "all", 1..len(nets) the networks.
	pos := 0
	active := m.activeNetwork()
	for i, n := range nets {
		if n == active {
			pos = i + 1
		}
	}
	slots := len(nets) + 1
	pos = ((pos+step)%slots + slots) % slots
	m.network = ""
	if pos > 0 {
		m.network = nets[pos-1]
	}
	m.refreshKeepingSelection()
	return m.network, true
}

// SetGroupByNetwork switches between the list's own order and grouping under
// a header per network.
func (m *SimpleListModel) SetGroupByNetwork(group bool) {
	if m.groupByNetwork == group {
		return
	}
	m.groupByNetwork = group
	m.refreshKeepingSelection()
}

func (m *SimpleListModel) GroupByNetwork() bool {
	return m.groupByNetwork
}

// SetPinned replaces the pinned chats, in display order.
func (m *SimpleListModel) SetPinned(guids []string) {
	m.pinned = append([]string(nil), guids...)
	m.refreshKeepingSelection()
}

// Pinned returns the pinned chat GUIDs in display order.
func (m *SimpleListModel) Pinned() []string {
	return append([]string(nil), m.pinned...)
}

func (m *SimpleListModel) IsPinned(chatGUID string) bool {
	for _, guid := range m.pinned {
		if guid == chatGUID {
			return true
		}
	}
	return false
}

// TogglePin pins a chat at the end of the Pinned section, or unpins it, and
// reports whether it is pinned now. The cursor follows the chat.
func (m *SimpleListModel) TogglePin(chatGUID string) bool {
	pinned := !m.IsPinned(chatGUID)
	if pinned {
		m.pinned = append(m.pinned, chatGUID)
	} else {
		kept := m.pinned[:0]
		for _, guid := range m.pinned {
			if guid != chatGUID {
				kept = append(kept, guid)
			}
		}
		m.pinned = kept
	}
	m.refreshSelecting(chatGUID)
	return pinned
}

// SetIgnored replaces the ignored chats.
func (m *SimpleListModel) SetIgnored(guids []string) {
	m.setIgnored(guids)
	m.refreshKeepingSelection()
}

func (m *SimpleListModel) setIgnored(guids []string) {
	m.ignored = append([]string(nil), guids...)
	m.ignoredSet = make(map[string]bool, len(guids))
	for _, guid := range guids {
		m.ignoredSet[guid] = true
	}
}

// Ignored returns the ignored chat GUIDs in the order they were ignored.
func (m *SimpleListModel) Ignored() []string {
	return append([]string(nil), m.ignored...)
}

func (m *SimpleListModel) IsIgnored(chatGUID string) bool {
	return m.ignoredSet[chatGUID]
}

// ToggleIgnore ignores a chat, or stops ignoring it, and reports whether it is
// ignored now. Ignoring also unpins: a favourite that is never shown is not
// one. When the chat leaves the list the cursor keeps its place, landing on
// the row that took the chat's, rather than jumping back to the top.
func (m *SimpleListModel) ToggleIgnore(chatGUID string) bool {
	ignore := !m.ignoredSet[chatGUID]
	guids := m.Ignored()
	if ignore {
		guids = append(guids, chatGUID)
		if m.IsPinned(chatGUID) {
			m.TogglePin(chatGUID)
		}
	} else {
		kept := guids[:0]
		for _, guid := range guids {
			if guid != chatGUID {
				kept = append(kept, guid)
			}
		}
		guids = kept
	}
	m.setIgnored(guids)
	if ignore && !m.showIgnored {
		cursor := m.cursor
		m.rebuildFilter()
		m.cursor = cursor
		m.clampCursor()
	} else {
		m.refreshSelecting(chatGUID)
	}
	return ignore
}

// SetShowIgnored shows or hides the Ignored section.
func (m *SimpleListModel) SetShowIgnored(show bool) {
	if m.showIgnored == show {
		return
	}
	m.showIgnored = show
	m.refreshKeepingSelection()
}

func (m *SimpleListModel) ShowIgnored() bool {
	return m.showIgnored
}

// SetShowUnknown shows or hides the Unknown senders section.
func (m *SimpleListModel) SetShowUnknown(show bool) {
	if m.showUnknown == show {
		return
	}
	m.showUnknown = show
	m.refreshKeepingSelection()
}

func (m *SimpleListModel) ShowUnknown() bool {
	return m.showUnknown
}

// UnknownCount is how many chats from unknown senders the list holds, not
// counting ignored ones.
func (m *SimpleListModel) UnknownCount() int {
	n := 0
	for _, chat := range m.items {
		if chat.UnknownSender && !m.ignoredSet[chat.GUID] {
			n++
		}
	}
	return n
}

// counted reports whether a chat takes part in the list's bookkeeping —
// which networks there are, what is listed. Ignored chats only do while they
// are on show; unknown senders while on show, or when they have something
// new, or when pinned.
func (m *SimpleListModel) counted(chat models.Chat) bool {
	if m.ignoredSet[chat.GUID] {
		return m.showIgnored
	}
	if chat.UnknownSender && !chat.HasNewMessage && !m.IsPinned(chat.GUID) {
		return m.showUnknown
	}
	return true
}

// MovePin moves a pinned chat one place up (step -1) or down (step 1) in the
// Pinned section, and reports whether it moved. It trades places with the
// nearest pin that is in view, so a filter hiding the neighbour does not turn
// the key into a silent no-op.
func (m *SimpleListModel) MovePin(chatGUID string, step int) bool {
	from := -1
	for i, guid := range m.pinned {
		if guid == chatGUID {
			from = i
		}
	}
	if from < 0 || step == 0 {
		return false
	}
	visible := make(map[string]bool, len(m.filteredIndices))
	for _, idx := range m.filteredIndices {
		visible[m.items[idx].GUID] = true
	}
	if step > 0 {
		step = 1
	} else {
		step = -1
	}
	for to := from + step; to >= 0 && to < len(m.pinned); to += step {
		if !visible[m.pinned[to]] {
			continue
		}
		m.pinned[from], m.pinned[to] = m.pinned[to], m.pinned[from]
		m.refreshSelecting(chatGUID)
		return true
	}
	return false
}

// NetworkCount is how many networks the listed chats come from.
func (m *SimpleListModel) NetworkCount() int {
	return len(m.networks())
}

func (m *SimpleListModel) chatNetwork(chat models.Chat) string {
	if m.networkOf == nil {
		return ""
	}
	return m.networkOf(chat.GUID)
}

// networks lists the networks present among the chats: those in networkOrder
// first, in that order, then any others alphabetically.
func (m *SimpleListModel) networks() []string {
	present := make(map[string]bool)
	for _, chat := range m.items {
		if m.counted(chat) {
			present[m.chatNetwork(chat)] = true
		}
	}
	out := make([]string, 0, len(present))
	for _, n := range m.networkOrder {
		if present[n] {
			out = append(out, n)
			delete(present, n)
		}
	}
	rest := make([]string, 0, len(present))
	for n := range present {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// activeNetwork is the filter in effect: the one set, if any chat has it.
func (m *SimpleListModel) activeNetwork() string {
	if m.network == "" {
		return ""
	}
	for _, chat := range m.items {
		if m.chatNetwork(chat) == m.network {
			return m.network
		}
	}
	return ""
}

func networkTitle(network string) string {
	if network == "" {
		return "Other"
	}
	return network
}

// refreshKeepingSelection rebuilds the view and keeps the cursor on the chat
// it was on, falling back to the top when that chat is filtered out.
func (m *SimpleListModel) refreshKeepingSelection() {
	m.refreshSelecting(m.selectedGUID())
}

func (m *SimpleListModel) selectedGUID() string {
	if selected := m.SelectedItem(); selected != nil {
		return selected.GUID
	}
	return ""
}

// refreshSelecting rebuilds the view with the cursor on guid. Callers that
// reorder items must read the selection first: the cursor indexes the old
// display order, which no longer matches the items.
func (m *SimpleListModel) refreshSelecting(guid string) {
	m.rebuildFilter()
	if guid == "" || !m.SelectByGUID(guid) {
		m.cursor = 0
		m.offset = 0
	}
	m.clampCursor()
}

func (m *SimpleListModel) StartSearch() {
	m.searchActive = true
	m.searchQuery = ""
	m.rebuildFilter()
	m.cursor = 0
	m.offset = 0
}

func (m *SimpleListModel) ClearSearch() {
	m.searchActive = false
	m.searchQuery = ""
	m.rebuildFilter()
	m.clampCursor()
}

func (m *SimpleListModel) SearchActive() bool {
	return m.searchActive
}

func (m *SimpleListModel) AppendSearch(r rune) {
	if !m.searchActive {
		return
	}
	m.searchQuery += string(r)
	m.rebuildFilter()
	m.cursor = 0
	m.offset = 0
}

func (m *SimpleListModel) BackspaceSearch() {
	if !m.searchActive || m.searchQuery == "" {
		return
	}
	runes := []rune(m.searchQuery)
	m.searchQuery = string(runes[:len(runes)-1])
	m.rebuildFilter()
	m.clampCursor()
}

func (m *SimpleListModel) rebuildFilter() {
	q := ""
	if m.searchActive {
		q = strings.ToLower(strings.TrimSpace(m.searchQuery))
	}
	network := m.activeNetwork()
	m.filteredIndices = m.filteredIndices[:0]
	for i, chat := range m.items {
		if !m.counted(chat) {
			continue
		}
		if network != "" && m.chatNetwork(chat) != network {
			continue
		}
		if q != "" && !chatMatchesQuery(chat, q) {
			continue
		}
		m.filteredIndices = append(m.filteredIndices, i)
	}
	m.rebuildRows()
}

type listSection struct {
	title   string
	network string // set when the section is one network's chats
	items   []int  // indices into items
}

// rebuildRows lays filteredIndices out as rows, in sections: pinned chats
// first, in pin order, then the rest — either as they are or, grouped, by
// network. Within a network the list's own order holds, so a chat that just
// got a message still tops its group. A pinned chat appears only under
// Pinned.
//
// Headers appear when there is something to tell apart: pinned chats in view,
// or grouping across more than one network. A plain list stays headerless.
func (m *SimpleListModel) rebuildRows() {
	m.rows = m.rows[:0]

	byGUID := make(map[string]int, len(m.filteredIndices))
	for _, idx := range m.filteredIndices {
		byGUID[m.items[idx].GUID] = idx
	}
	// Ignored chats only reach this far while on show. They go last, under
	// their own header, so un-ignoring one is a matter of finding it there.
	var ignored []int
	isIgnored := make(map[int]bool, len(m.ignored))
	for _, idx := range m.filteredIndices {
		if m.ignoredSet[m.items[idx].GUID] {
			ignored = append(ignored, idx)
			isIgnored[idx] = true
		}
	}
	var pins []int
	isPin := make(map[int]bool, len(m.pinned))
	for _, guid := range m.pinned {
		if idx, ok := byGUID[guid]; ok && !isPin[idx] && !isIgnored[idx] {
			pins = append(pins, idx)
			isPin[idx] = true
		}
	}
	// Unknown senders on show get a section of their own. One with a new
	// message stays among the rest, at the top where it can be seen.
	var unknown []int
	rest := make([]int, 0, len(m.filteredIndices)-len(pins)-len(ignored))
	for _, idx := range m.filteredIndices {
		if isPin[idx] || isIgnored[idx] {
			continue
		}
		if chat := m.items[idx]; chat.UnknownSender && !chat.HasNewMessage {
			unknown = append(unknown, idx)
			continue
		}
		rest = append(rest, idx)
	}

	var sections []listSection
	if len(pins) > 0 {
		sections = append(sections, listSection{title: "Pinned", items: pins})
	}
	networks := m.networks()
	active := m.activeNetwork()
	grouped := m.groupByNetwork && active == "" && len(networks) > 1
	switch {
	case grouped:
		groups := make(map[string][]int, len(networks))
		for _, idx := range rest {
			n := m.chatNetwork(m.items[idx])
			groups[n] = append(groups[n], idx)
		}
		for _, n := range networks {
			sections = append(sections, listSection{title: networkTitle(n), network: n, items: groups[n]})
		}
	case active != "":
		sections = append(sections, listSection{title: networkTitle(active), network: active, items: rest})
	default:
		sections = append(sections, listSection{title: "Recent", items: rest})
	}

	if len(unknown) > 0 {
		sections = append(sections, listSection{title: "Unknown senders", items: unknown})
	}
	if len(ignored) > 0 {
		sections = append(sections, listSection{title: "Ignored", items: ignored})
	}

	headers := grouped || len(pins) > 0 || len(unknown) > 0 || len(ignored) > 0
	ordered := make([]int, 0, len(m.filteredIndices))
	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		if headers {
			m.rows = append(m.rows, listRow{header: section.title, network: section.network, item: -1})
		}
		for _, idx := range section.items {
			m.rows = append(m.rows, listRow{item: len(ordered)})
			ordered = append(ordered, idx)
		}
	}
	m.filteredIndices = ordered
}

func chatMatchesQuery(chat models.Chat, q string) bool {
	if strings.Contains(strings.ToLower(stripEmojis(chat.GetDisplayName())), q) {
		return true
	}
	if strings.Contains(strings.ToLower(chat.ChatIdentifier), q) {
		return true
	}
	for _, p := range chat.Participants {
		if strings.Contains(strings.ToLower(p.DisplayName), q) {
			return true
		}
		if strings.Contains(strings.ToLower(p.Address), q) {
			return true
		}
	}
	return false
}

func (m *SimpleListModel) clampCursor() {
	if len(m.filteredIndices) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= len(m.filteredIndices) {
		m.cursor = len(m.filteredIndices) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.offset >= len(m.rows) {
		m.offset = 0
	}
	m.ensureCursorVisible()
}

func (m *SimpleListModel) linesPerItem() int {
	if m.showPreview {
		return 2
	}
	return 1
}

// bodyLines is the height left for rows under the title, the network tabs
// and the search bar.
func (m *SimpleListModel) bodyLines() int {
	reserved := 1 + m.tabLineCount() // title, tabs
	if m.searchActive {
		reserved++
	}
	return m.height - reserved
}

func (m *SimpleListModel) rowLines(row listRow) int {
	if row.item < 0 {
		return 1
	}
	return m.linesPerItem()
}

func (m *SimpleListModel) cursorRow() int {
	for i, row := range m.rows {
		if row.item == m.cursor {
			return i
		}
	}
	return -1
}

// ensureCursorVisible scrolls just enough to show the cursor's row, and the
// header of its group when the cursor is on the group's first chat.
func (m *SimpleListModel) ensureCursorVisible() {
	row := m.cursorRow()
	if row < 0 {
		m.offset = 0
		return
	}
	top := row
	if top > 0 && m.rows[top-1].item < 0 {
		top--
	}
	if top < m.offset {
		m.offset = top
	}
	budget := max(m.bodyLines(), m.linesPerItem())
	used := 0
	for i := m.offset; i <= row; i++ {
		used += m.rowLines(m.rows[i])
	}
	for m.offset < row && used > budget {
		used -= m.rowLines(m.rows[m.offset])
		m.offset++
	}
}

func (m *SimpleListModel) SelectedItem() *models.Chat {
	if m.cursor >= 0 && m.cursor < len(m.filteredIndices) {
		idx := m.filteredIndices[m.cursor]
		if idx >= 0 && idx < len(m.items) {
			return &m.items[idx]
		}
	}
	return nil
}

func (m *SimpleListModel) UpdateChatPreview(chatGUID, text string, when int64) {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.items[i].LastMessageText = text
		if when > 0 {
			m.items[i].LastMessageTime = when
		}
		return
	}
}

// SelectByGUID moves the list cursor to the chat with the given GUID.
func (m *SimpleListModel) SelectByGUID(chatGUID string) bool {
	for fi, idx := range m.filteredIndices {
		if m.items[idx].GUID != chatGUID {
			continue
		}
		m.cursor = fi
		m.ensureCursorVisible()
		return true
	}
	return false
}

func (m *SimpleListModel) MarkNewMessage(chatGUID string) {
	for i, chat := range m.items {
		if chat.GUID != chatGUID {
			continue
		}
		m.items[i].HasNewMessage = true
		m.items[i].UnreadCount++
		// Move it to the top — of its group, when grouped, since groups keep
		// this order — and keep the cursor on the chat it was on. Rebuild
		// even when it is on top already: a new message is what lists an
		// unknown sender.
		selected := m.selectedGUID()
		chat = m.items[i]
		copy(m.items[1:i+1], m.items[0:i])
		m.items[0] = chat
		m.refreshSelecting(selected)
		return
	}
}

// ClickAt handles a click at column x, row y of the list's content, and
// reports whether it changed the network filter — the caller saves that.
//
// A network tab filters to its network. A network's section header does the
// same, and clicking the header of the network already shown goes back to
// all of them. Pinned and Recent headers select nothing.
func (m *SimpleListModel) ClickAt(x, y int) bool {
	if m.searchActive {
		searchY := m.height - 1
		if y >= searchY {
			return false
		}
	}
	tabs := m.networkTabs()
	tabRows := tabLineCount(tabs)
	if y >= 1 && y < 1+tabRows {
		for _, tab := range tabs {
			if tab.line == y-1 && x >= tab.x0 && x < tab.x1 {
				return m.selectNetwork(tab.network)
			}
		}
		return false
	}
	line := y - 1 - tabRows
	if line < 0 {
		return false
	}
	for ri := m.offset; ri < len(m.rows); ri++ {
		row := m.rows[ri]
		h := m.rowLines(row)
		if line < h {
			if row.item >= 0 {
				m.cursor = row.item
				return false
			}
			if row.network == "" {
				return false
			}
			target := row.network
			if m.activeNetwork() == target {
				target = ""
			}
			return m.selectNetwork(target)
		}
		line -= h
	}
	return false
}

func (m *SimpleListModel) selectNetwork(network string) bool {
	changed := m.network != network
	m.SetNetwork(network)
	return changed
}

// networkTabs lays out the filter chips — All, then each network in group
// order — wrapping onto more lines when the pane is too narrow for one. A
// network with new messages carries its count, except the one being shown.
// There are no tabs with fewer than two networks: nothing to choose between.
func (m SimpleListModel) networkTabs() []networkTab {
	var nets []string
	for _, n := range m.networks() {
		if n != "" {
			nets = append(nets, n)
		}
	}
	if len(nets) < 2 {
		return nil
	}
	unread := make(map[string]int, len(nets))
	for _, chat := range m.items {
		if chat.HasNewMessage && !m.ignoredSet[chat.GUID] {
			unread[m.chatNetwork(chat)]++
		}
	}
	width := 1 << 20 // unsized: nothing to wrap to
	if m.width > 0 {
		width = max(m.width-1, 2)
	}
	tabs := make([]networkTab, 0, len(nets)+1)
	line, x := 0, 1 // one column in, like the chat rows
	add := func(network, label string, count int) {
		text := " " + label + " "
		if count > 0 {
			text = " " + label + " " + itoa(count) + " "
		}
		text = truncateToWidth(text, width-1)
		w := lipgloss.Width(text)
		if x > 1 && x+w > width {
			line++
			x = 1
		}
		tabs = append(tabs, networkTab{network: network, text: text, unread: count, line: line, x0: x, x1: x + w})
		x += w + 1
	}
	add("", "All", 0)
	active := m.activeNetwork()
	for _, n := range nets {
		count := unread[n]
		if n == active {
			count = 0
		}
		add(n, n, count)
	}
	return tabs
}

func tabLineCount(tabs []networkTab) int {
	if len(tabs) == 0 {
		return 0
	}
	return tabs[len(tabs)-1].line + 1
}

func (m SimpleListModel) tabLineCount() int {
	return tabLineCount(m.networkTabs())
}

func (m SimpleListModel) tabLines() []string {
	tabs := m.networkTabs()
	lines := make([]string, tabLineCount(tabs))
	col := make([]int, len(lines))
	active := m.activeNetwork()
	for _, tab := range tabs {
		style := m.timeStyle
		switch {
		case tab.network == active:
			style = m.selectedStyle
		case tab.unread > 0:
			style = m.newMessageStyle
		}
		lines[tab.line] += strings.Repeat(" ", tab.x0-col[tab.line]) + style.Render(tab.text)
		col[tab.line] = tab.x1
	}
	return lines
}

func (m SimpleListModel) writeHead(b *strings.Builder) {
	b.WriteString(m.titleLine())
	b.WriteString("\n")
	for _, line := range m.tabLines() {
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func (m *SimpleListModel) ClearNewMessage(chatGUID string) {
	for i, chat := range m.items {
		if chat.GUID == chatGUID {
			m.items[i].HasNewMessage = false
			m.items[i].UnreadCount = 0
			return
		}
	}
}

func (m *SimpleListModel) NewMessageCount() int {
	count := 0
	for _, chat := range m.items {
		if chat.HasNewMessage && !m.ignoredSet[chat.GUID] {
			count++
		}
	}
	return count
}

func chatListPrefix(chat models.Chat) string {
	if chat.IsGroup() {
		n := len(chat.Participants)
		if n > 1 {
			return "(" + itoa(n) + ") "
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func previewText(chat models.Chat) string {
	text := strings.TrimSpace(chat.LastMessageText)
	if text == "" && chat.LastMessage != nil {
		text = strings.TrimSpace(chat.LastMessage.Text)
	}
	if text == "" {
		if chat.HasNewMessage {
			return "New message"
		}
		return ""
	}
	return truncatePreview(text, 40)
}

func (m SimpleListModel) Update(msg tea.Msg) (SimpleListModel, tea.Cmd) {
	if m.searchActive {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.ClearSearch()
				return m, nil
			case "backspace", "ctrl+h":
				m.BackspaceSearch()
				return m, nil
			case "enter":
				m.ClearSearch()
				return m, nil
			default:
				if len(msg.Runes) == 1 {
					m.AppendSearch(msg.Runes[0])
				}
				return m, nil
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.ensureCursorVisible()
			}
		case "down", "j":
			if m.cursor < len(m.filteredIndices)-1 {
				m.cursor++
				m.ensureCursorVisible()
			}
		case "g":
			m.cursor = 0
			m.offset = 0
		case "G":
			m.cursor = len(m.filteredIndices) - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.ensureCursorVisible()
		}
	}
	return m, nil
}

func (m SimpleListModel) View() string {
	if m.loading && len(m.items) == 0 {
		return lipgloss.NewStyle().Foreground(ColorWindowPlaceholder).Render("Loading chats…")
	}
	if len(m.items) == 0 {
		return "No chats"
	}
	if len(m.filteredIndices) == 0 {
		var b strings.Builder
		m.writeHead(&b)
		b.WriteString(m.searchStyle.Render("  No matches"))
		if m.searchActive {
			b.WriteString("\n")
			b.WriteString(m.searchBar())
		}
		return b.String()
	}

	var b strings.Builder
	m.writeHead(&b)

	nameWidth := m.width - 8
	if nameWidth < 8 {
		nameWidth = 8
	}

	budget := m.bodyLines()
	used := 0
	for ri := m.offset; ri < len(m.rows); ri++ {
		row := m.rows[ri]
		if used+m.rowLines(row) > budget {
			break
		}
		used += m.rowLines(row)
		if row.item < 0 {
			b.WriteString(m.headerLine(row.header))
			b.WriteString("\n")
			continue
		}
		fi := row.item
		idx := m.filteredIndices[fi]
		chat := m.items[idx]
		displayName := chatListPrefix(chat) + stripEmojis(chat.GetDisplayName())

		linePrefix := ""
		if chat.HasNewMessage {
			linePrefix = "● "
		}
		lineSuffix := ""
		if chat.UnreadCount > 1 {
			lineSuffix = " (" + itoa(chat.UnreadCount) + ")"
		}

		lineWidth := m.width
		if lineWidth > 1 {
			lineWidth--
		}

		timeStr := ""
		timeStyled := m.timeStyle.Render(formatChatListTime(chat.LastMessageTime))
		reserved := 1 + lipgloss.Width(linePrefix) + lipgloss.Width(lineSuffix)
		if lipgloss.Width(timeStyled) > 0 && lineWidth-reserved-lipgloss.Width(timeStyled)-1 >= 1 {
			timeStr = timeStyled
			reserved += lipgloss.Width(timeStyled) + 1
		}
		nameWidth = lineWidth - reserved
		if nameWidth < 1 {
			nameWidth = 1
		}
		displayName = truncatePreview(displayName, nameWidth)

		line1 := linePrefix + displayName + lineSuffix
		if timeStr != "" {
			gap := lineWidth - lipgloss.Width(" "+line1) - lipgloss.Width(timeStr)
			if gap < 1 {
				gap = 1
			}
			timeStr = strings.Repeat(" ", gap) + timeStr
		}

		if fi == m.cursor {
			line1 = m.selectedStyle.Render(" " + line1 + timeStr)
		} else if chat.HasNewMessage {
			line1 = m.newMessageStyle.Render(" "+line1) + timeStr
		} else {
			line1 = m.normalStyle.Render(" "+line1) + timeStr
		}
		b.WriteString(line1)
		b.WriteString("\n")

		if m.showPreview {
			preview := previewText(chat)
			if preview != "" {
				preview = truncatePreview(preview, nameWidth)
				previewLine := m.previewStyle.Render("   " + preview)
				if fi == m.cursor {
					previewLine = m.selectedStyle.Render("   " + preview)
				}
				b.WriteString(previewLine)
			}
			b.WriteString("\n")
		}
	}

	if m.searchActive {
		b.WriteString(m.searchBar())
	}
	return b.String()
}

func (m SimpleListModel) titleLine() string {
	title := "CHATS"
	if m.focused {
		title = "▸ " + title
	}
	titleStyled := lipgloss.NewStyle().Bold(true).Render(title)
	if m.loading {
		titleStyled += lipgloss.NewStyle().Foreground(ColorWindowPlaceholder).Render(" …")
	}
	return m.fitLine(titleStyled)
}

func (m SimpleListModel) headerLine(network string) string {
	return m.headerStyle.Render(m.fitLine(" " + network))
}

// fitLine cuts a line one column short of the pane, the margin the chat rows
// keep. Before the pane has a size there is nothing to fit to.
func (m SimpleListModel) fitLine(line string) string {
	if m.width <= 0 {
		return line
	}
	return truncateToWidth(line, max(m.width-1, 1))
}

func (m SimpleListModel) searchBar() string {
	q := m.searchQuery
	if q == "" {
		q = "_"
	}
	return m.searchStyle.Render("/" + q)
}
