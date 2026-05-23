package tui

import (
	"strings"
	"testing"

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
