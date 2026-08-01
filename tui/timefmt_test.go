package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestFormatChatListDateUsesNumericDayMonth(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.Local)
	msgTime := time.Date(2026, 5, 21, 9, 30, 0, 0, time.Local)

	if got, want := formatChatListDate(msgTime, now), "21/5"; got != want {
		t.Fatalf("chat list date = %q, want %q", got, want)
	}
}

func TestTruncateToWidthCountsColumnsNotRunes(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
	}{
		{"plain emoji", "😂😂😂😂😂😂😂😂 hej", 12},
		{"skin tone modifier", "👍🏽👍🏽👍🏽👍🏽 tummen upp", 9},
		{"zwj sequence", "👨‍👩‍👧‍👦👨‍👩‍👧‍👦 familjen", 7},
		{"variation selector", "❤️❤️❤️❤️❤️ hjärtan", 6},
		{"ascii", "abcdefghijklmnop", 10},
		{"mixed", "hej 😂 då ❤️ igen", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateToWidth(tc.text, tc.width)
			if w := lipgloss.Width(got); w > tc.width {
				t.Fatalf("truncateToWidth(%q, %d) = %q, width %d exceeds %d",
					tc.text, tc.width, got, w, tc.width)
			}
			if !strings.HasSuffix(got, "…") {
				t.Fatalf("expected an ellipsis to mark the cut: %q", got)
			}
		})
	}
}

func TestTruncateToWidthLeavesShortTextAlone(t *testing.T) {
	if got := truncateToWidth("hej 😂", 10); got != "hej 😂" {
		t.Fatalf("text that fits was altered: %q", got)
	}
	if got := truncateToWidth("hej", 0); got != "" {
		t.Fatalf("zero width should render nothing, got %q", got)
	}
}
