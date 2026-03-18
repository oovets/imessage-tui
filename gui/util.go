package gui

import (
	"strings"
	"time"
	"unicode"
)

// stripEmojis removes emoji and symbol characters, keeping only letters, digits,
// spaces, and common punctuation. Ported from tui/simplelist.go.
func stripEmojis(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(r)
		case r == '-' || r == '\'' || r == '.' || r == ',' || r == '(' || r == ')':
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func formatMessageTime(t time.Time) string {
	return t.Format("15:04")
}

// formatHoverTimestamp returns a compact timestamp for hover tooltips.
// For today's messages it shows only time; older messages include date.
func formatHoverTimestamp(t time.Time) string {
	now := time.Now()
	local := t.Local()
	if now.Year() == local.Year() && now.YearDay() == local.YearDay() {
		return local.Format("15:04")
	}
	if now.Year() == local.Year() {
		return local.Format("Jan 2 15:04")
	}
	return local.Format("2006-01-02 15:04")
}

func truncateString(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}
