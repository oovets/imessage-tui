package tui

import (
	"strings"
	"testing"

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
