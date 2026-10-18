package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

func TestSimpleListModelCanHideChatPreview(t *testing.T) {
	model := NewSimpleListModel()
	model.SetSize(40, 8)
	model.SetItems([]models.Chat{
		{
			GUID:            "chat-a",
			DisplayName:     "Alice",
			LastMessageText: "preview text",
			LastMessageTime: 1000,
		},
	})

	if !strings.Contains(model.View(), "preview text") {
		t.Fatal("expected preview to render by default")
	}

	model.SetShowPreview(false)
	if strings.Contains(model.View(), "preview text") {
		t.Fatal("expected preview to be hidden")
	}
}

func TestSimpleListModelWithoutPreviewKeepsRowsWithinWidth(t *testing.T) {
	model := NewSimpleListModel()
	model.SetShowPreview(false)
	model.SetSize(12, 8)
	model.SetItems([]models.Chat{
		{
			GUID:            "chat-a",
			DisplayName:     "Very Long Chat Name",
			LastMessageTime: 1000,
		},
	})

	for _, line := range strings.Split(model.View(), "\n") {
		if got := lipgloss.Width(line); got >= 12 {
			t.Fatalf("line width = %d, want < 12: %q", got, line)
		}
	}
}

func TestAppChatListWithoutPreviewKeepsTimestampOnSameRenderedRow(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	app.width = 60
	app.height = 10
	app.showChatList = true
	app.showChatPreview = false
	app.chatListWidth = 12
	app.focused = focusChatList
	app.chatList.SetChats([]models.Chat{
		{
			GUID:            "chat-a",
			DisplayName:     "Very Long Chat Name",
			LastMessageTime: 1000,
		},
	})
	app.chatList.SetShowPreview(false)
	app.updateLayout()

	if got, want := app.chatList.list.width, app.chatListWidth-3; got != want {
		t.Fatalf("chat list inner width = %d, want %d", got, want)
	}
	for _, line := range strings.Split(stripANSI(app.View()), "\n") {
		if got := lipgloss.Width(line); got > app.width {
			t.Fatalf("rendered line width = %d, want <= %d: %q", got, app.width, line)
		}
		if strings.TrimSpace(line) == "1/1/70" {
			t.Fatalf("timestamp rendered on its own row: %q", app.View())
		}
	}
}

func TestSimpleListModelGroupPrefixShowsMemberCountWithoutHash(t *testing.T) {
	model := NewSimpleListModel()
	model.SetSize(40, 8)
	model.SetItems([]models.Chat{
		{
			GUID:        "group-a",
			DisplayName: "Group",
			Participants: []models.Handle{
				{DisplayName: "Alice"},
				{DisplayName: "Bob"},
				{DisplayName: "Cara"},
			},
		},
	})

	view := model.View()
	if !strings.Contains(view, "(3)") {
		t.Fatalf("expected member count prefix: %q", view)
	}
	if strings.Contains(view, "#") {
		t.Fatalf("expected group prefix without hash: %q", view)
	}
}

func TestChatListLinesStayInsideWidthWithEmoji(t *testing.T) {
	const width = 25

	model := NewSimpleListModel()
	model.SetSize(width, 12)
	model.SetShowPreview(true)
	model.SetItems([]models.Chat{
		{
			GUID:            "chat-emoji",
			DisplayName:     "Familjen",
			LastMessageText: "😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂😂 hej",
			LastMessageTime: 1000,
		},
		{
			GUID:            "chat-clusters",
			DisplayName:     "Jobb",
			LastMessageText: "👨‍👩‍👧‍👦👍🏽❤️✅👨‍👩‍👧‍👦👍🏽❤️✅👨‍👩‍👧‍👦👍🏽❤️✅ okej",
			LastMessageTime: 2000,
		},
	})

	// An emoji is two columns wide, so a preview truncated by rune count comes
	// out twice as wide as its column. lipgloss.JoinHorizontal then sizes the
	// whole pane to the widest line, which shoves the divider and every pane
	// right of it sideways.
	for i, line := range strings.Split(model.View(), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("chat list line %d is %d columns wide, want at most %d: %q",
				i, w, width, line)
		}
	}
}

// A chat with nothing to preview takes one line, not a name and a blank line:
// Slack conversations arrive without their last message, so with previews on
// every one of them used to leave an empty row under it.
func TestChatListLeavesNoBlankRowForChatsWithoutPreview(t *testing.T) {
	chats := []models.Chat{
		{GUID: "a", DisplayName: "Alice", LastMessageText: "hej", LastMessageTime: 6000},
		{GUID: "b", DisplayName: "general", LastMessageTime: 5000},
		{GUID: "c", DisplayName: "random", LastMessageTime: 4000},
		{GUID: "d", DisplayName: "Bob", LastMessageText: "ses", LastMessageTime: 3000},
		{GUID: "e", DisplayName: "design", LastMessageTime: 2000},
		{GUID: "f", DisplayName: "ops", LastMessageTime: 1000},
	}
	m := NewSimpleListModel()
	m.SetShowPreview(true)
	m.SetSize(30, 20)
	m.SetItems(chats)

	lines := strings.Split(strings.TrimRight(stripANSI(m.View()), "\n"), "\n")
	// Title, then each chat on one line plus a line for each preview.
	if want := 1 + len(chats) + 2; len(lines) != want {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), want, strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("blank line %d:\n%s", i, strings.Join(lines, "\n"))
		}
	}

	// With rows of two heights, a click still selects the chat drawn on the
	// line clicked, and a click on a preview line selects the chat above it.
	owner := ""
	for y, line := range lines[1:] {
		for _, chat := range chats {
			if strings.Contains(line, chat.DisplayName) {
				owner = chat.GUID
			}
		}
		m.ClickAt(3, y+1)
		if got := m.SelectedItem().GUID; got != owner {
			t.Errorf("click on line %d %q selected %s, want %s", y+1, line, got, owner)
		}
	}
}

// Scrolling budgets each row by its own height, so moving down a list taller
// than the pane keeps the cursor's chat on screen.
func TestChatListScrollKeepsCursorVisibleWithMixedRowHeights(t *testing.T) {
	var chats []models.Chat
	for i := 0; i < 12; i++ {
		chat := models.Chat{GUID: string(rune('a' + i)), DisplayName: "chat-" + string(rune('a'+i)), LastMessageTime: int64(100 - i)}
		if i%3 == 0 {
			chat.LastMessageText = "preview"
		}
		chats = append(chats, chat)
	}
	m := NewSimpleListModel()
	m.SetShowPreview(true)
	m.SetSize(30, 6)
	m.SetItems(chats)

	for i := 0; i < len(chats); i++ {
		name := m.SelectedItem().DisplayName
		view := stripANSI(m.View())
		if !strings.Contains(view, name) {
			t.Fatalf("cursor on %s, which is off screen:\n%s", name, view)
		}
		if n := strings.Count(view, "\n"); n > 6 {
			t.Fatalf("view is %d lines in a 6-line pane:\n%s", n, view)
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}
