package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/enescakir/emoji"
)

// acMaxResults caps how many emoji suggestions we keep for a query. The popup
// scrolls if the selection moves past what fits on screen.
const acMaxResults = 8

// acMinQuery is the number of characters after ':' before the popup appears.
// Keeping this at 2 avoids a giant flashing list on every single ":x".
const acMinQuery = 2

type emojiEntry struct {
	name  string // shortcode without the surrounding colons, e.g. "smile"
	glyph string // the rendered emoji, e.g. "😄"
}

// emojiIndex is the searchable, alphabetically sorted set of emoji we offer for
// autocomplete. Built once at startup from the enescakir/emoji shortcode map.
var emojiIndex []emojiEntry

func init() {
	m := emoji.Map()
	emojiIndex = make([]emojiEntry, 0, len(m))
	for code, glyph := range m {
		name := strings.Trim(code, ":")
		if name == "" {
			continue
		}
		// Skin-tone variants (":+1_tone3:") just clutter the list with
		// near-duplicates of the base emoji.
		if strings.Contains(name, "_tone") {
			continue
		}
		// ZWJ sequences (families, multi-person, profession combos) render
		// inconsistently across terminals and drift the cursor, so we keep the
		// picker to single, well-behaved glyphs for v1.
		if strings.ContainsRune(glyph, '‍') {
			continue
		}
		emojiIndex = append(emojiIndex, emojiEntry{name: name, glyph: glyph})
	}
	sort.Slice(emojiIndex, func(i, j int) bool {
		return emojiIndex[i].name < emojiIndex[j].name
	})
}

// searchEmoji returns up to limit matches for query, prefix matches first then
// substring matches, each group in alphabetical order.
func searchEmoji(query string, limit int) []emojiEntry {
	query = strings.ToLower(query)
	var prefix, contains []emojiEntry
	for _, e := range emojiIndex {
		switch {
		case strings.HasPrefix(e.name, query):
			prefix = append(prefix, e)
		case strings.Contains(e.name, query):
			contains = append(contains, e)
		}
		if len(prefix) >= limit {
			break
		}
	}
	out := prefix
	for _, e := range contains {
		if len(out) >= limit {
			break
		}
		out = append(out, e)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// isEmojiNameRune reports whether r can appear inside an emoji shortcode token
// being typed (so we know where the ':...' query starts and stops).
func isEmojiNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '+' || r == '-':
		return true
	}
	return false
}

// currentEmojiQuery inspects the text immediately before the cursor on the
// current logical line and, if it looks like an in-progress ':shortcode'
// token, returns the query (the part after ':'). The logical column is
// reconstructed from LineInfo so this stays correct even when the line soft
// wraps.
// lineBeforeCursor returns the runes on the current logical line up to the
// cursor. The logical column is reconstructed from LineInfo so it stays correct
// even when the line soft wraps.
func (m *InputModel) lineBeforeCursor() ([]rune, bool) {
	row := m.textarea.Line()
	lines := strings.Split(m.textarea.Value(), "\n")
	if row < 0 || row >= len(lines) {
		return nil, false
	}
	li := m.textarea.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	runes := []rune(lines[row])
	if col < 0 || col > len(runes) {
		return nil, false
	}
	return runes[:col], true
}

func (m *InputModel) currentEmojiQuery() (string, bool) {
	before, ok := m.lineBeforeCursor()
	if !ok {
		return "", false
	}

	colon := -1
	for i := len(before) - 1; i >= 0; i-- {
		r := before[i]
		if r == ':' {
			colon = i
			break
		}
		if !isEmojiNameRune(r) {
			return "", false
		}
	}
	if colon < 0 {
		return "", false
	}
	// The ':' must start a word — at line start or after whitespace — so we
	// don't fire inside things like "10:30" or URLs.
	if colon > 0 && !unicode.IsSpace(before[colon-1]) {
		return "", false
	}

	query := string(before[colon+1:])
	if len(query) < acMinQuery {
		return "", false
	}
	return query, true
}

// refreshAutocomplete recomputes the suggestion state from the current input.
// Call it after every textarea update.
func (m *InputModel) refreshAutocomplete() {
	query, ok := m.currentEmojiQuery()
	if !ok {
		m.dismissAutocomplete()
		return
	}
	if query != m.acQuery || !m.acActive {
		m.acQuery = query
		m.acMatches = searchEmoji(query, acMaxResults)
		m.acSelected = 0
	}
	m.acActive = len(m.acMatches) > 0
}

func (m *InputModel) dismissAutocomplete() {
	m.acActive = false
	m.acQuery = ""
	m.acMatches = nil
	m.acSelected = 0
}

// AutocompleteActive reports whether the suggestion popup is currently showing.
func (m InputModel) AutocompleteActive() bool {
	return m.acActive && len(m.acMatches) > 0
}

func (m *InputModel) moveAutocomplete(delta int) {
	if !m.acActive || len(m.acMatches) == 0 {
		return
	}
	n := len(m.acMatches)
	m.acSelected = ((m.acSelected+delta)%n + n) % n
}

// acceptAutocomplete replaces the typed ':query' token with the selected emoji
// glyph (plus a trailing space, matching Slack/Discord behaviour).
func (m *InputModel) acceptAutocomplete() {
	if !m.acActive || m.acSelected < 0 || m.acSelected >= len(m.acMatches) {
		return
	}
	glyph := m.acMatches[m.acSelected].glyph

	// Delete the ':' plus the query the user typed, then drop the emoji in its
	// place. Backspacing through the public Update keeps the cursor and wrap
	// state consistent without reaching into textarea internals.
	del := len([]rune(m.acQuery)) + 1
	for i := 0; i < del; i++ {
		m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m.textarea.InsertString(glyph)

	m.dismissAutocomplete()
	m.reflowHeight()
}

// HandleAutocompleteKey consumes navigation/accept/dismiss keys while the popup
// is open. It returns handled=true when the key was used for the popup (and
// must not fall through to the normal input/app key handling).
func (m *InputModel) HandleAutocompleteKey(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	if !m.AutocompleteActive() {
		return false, nil
	}
	switch msg.String() {
	case "up", "ctrl+p":
		m.moveAutocomplete(-1)
		return true, nil
	case "down", "ctrl+n":
		m.moveAutocomplete(1)
		return true, nil
	case "tab", "enter":
		m.acceptAutocomplete()
		return true, nil
	case "esc":
		m.dismissAutocomplete()
		return true, nil
	}
	return false, nil
}

// AutocompleteView renders the suggestion popup, sized to fit within maxWidth
// columns and maxRows suggestion lines (excluding its border). It returns ""
// when there is nothing to show.
func (m InputModel) AutocompleteView(maxWidth, maxRows int) string {
	if !m.AutocompleteActive() || maxRows < 1 || maxWidth < 6 {
		return ""
	}

	rows := maxRows
	if rows > len(m.acMatches) {
		rows = len(m.acMatches)
	}

	// Scroll the window so the selected entry stays visible.
	start := 0
	if m.acSelected >= rows {
		start = m.acSelected - rows + 1
	}

	innerWidth := 32
	if innerWidth > maxWidth-2 {
		innerWidth = maxWidth - 2
	}
	if innerWidth < 4 {
		innerWidth = 4
	}

	var b strings.Builder
	for i := start; i < start+rows; i++ {
		e := m.acMatches[i]
		label := truncateToWidth(fmt.Sprintf("%s  :%s:", e.glyph, e.name), innerWidth-1)
		rowStyle := lipgloss.NewStyle().Width(innerWidth)
		if i == m.acSelected {
			rowStyle = rowStyle.
				Background(ColorChatListSelectedBackground).
				Foreground(lipgloss.Color("231"))
		}
		b.WriteString(rowStyle.Render(" " + label))
		if i < start+rows-1 {
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorChatListSelectedBackground).
		Background(lipgloss.Color("235")).
		Render(b.String())
}

// truncateToWidth shortens s to at most width display columns, appending an
// ellipsis when it had to cut.
func truncateToWidth(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
