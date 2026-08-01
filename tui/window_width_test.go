package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

// A pane that renders even one line wider than its bounds makes
// lipgloss.JoinHorizontal size the whole block to that line, which bends the
// divider column and shifts every pane to its right.
func TestChatWindowViewStaysInsideBoundsWithEmoji(t *testing.T) {
	const width, height = 34, 14

	chat := models.Chat{
		GUID:        "chat-emoji",
		DisplayName: "🎉🎉🎉 Familjen 👨‍👩‍👧‍👦👨‍👩‍👧‍👦 långt namn som inte får plats",
	}

	w := NewChatWindow(1)
	w.Chat = &chat
	w.PaneIndex = 1
	w.PaneTotal = 2
	w.SetBounds(0, 0, width, height)
	w.Messages.SetMessages([]models.Message{
		{GUID: "m1", Text: "😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂 skratt", IsFromMe: true, DateCreated: 1000},
		{GUID: "m2", Text: "👍🏽❤️✅👨‍👩‍👧‍👦 blandat innehåll med kluster", DateCreated: 2000},
	})

	for i, line := range strings.Split(w.View(), "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("pane line %d is %d columns wide, want at most %d: %q",
				i, got, width, stripANSI(line))
		}
	}
}
