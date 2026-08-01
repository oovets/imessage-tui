package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func formatChatListTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	now := time.Now()
	today := truncateDay(now)
	msgDay := truncateDay(t)

	switch {
	case msgDay.Equal(today):
		return t.Format("15:04")
	case msgDay.Equal(today.AddDate(0, 0, -1)):
		return "yday"
	default:
		return formatChatListDate(t, now)
	}
}

func formatChatListDate(t, now time.Time) string {
	if now.Year() == t.Year() {
		return t.Format("2/1")
	}
	return t.Format("2/1/06")
}

func formatDateSeparator(t time.Time) string {
	now := time.Now()
	today := truncateDay(now)
	msgDay := truncateDay(t)

	switch {
	case msgDay.Equal(today):
		return "Today"
	case msgDay.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	default:
		if now.Year() == t.Year() {
			return t.Format("Monday, Jan 2")
		}
		return t.Format("Monday, Jan 2, 2006")
	}
}

func truncateDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func truncatePreview(text string, maxWidth int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return ""
	}
	return truncateToWidth(text, maxWidth)
}

// truncateToWidth shortens s to at most width terminal columns, appending an
// ellipsis when it had to cut.
//
// Columns, not runes: an emoji occupies two cells, and a cluster like 👍🏽 or
// 👨‍👩‍👧‍👦 is several runes rendered as one two-cell glyph. Counting runes let an
// emoji-heavy chat preview come out twice as wide as its column, which widened
// the whole pane and dragged the divider and everything right of it sideways.
// Cutting by runes could also split a cluster and leave a stray modifier
// behind, shifting the rest of the line by a cell.
func truncateToWidth(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// ansi.Truncate cuts on grapheme boundaries and reserves room for the tail.
	return ansi.Truncate(s, width, "…")
}
