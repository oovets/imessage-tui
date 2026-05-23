package tui

import (
	"testing"
	"time"
)

func TestFormatChatListDateUsesNumericDayMonth(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.Local)
	msgTime := time.Date(2026, 5, 21, 9, 30, 0, 0, time.Local)

	if got, want := formatChatListDate(msgTime, now), "21/5"; got != want {
		t.Fatalf("chat list date = %q, want %q", got, want)
	}
}
