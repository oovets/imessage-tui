package tui

import (
	"strings"
	"time"
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

func truncatePreview(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes-1]) + "…"
}
