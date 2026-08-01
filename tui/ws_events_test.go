package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oovets/imessage-tui/models"
)

func TestAppModelUpdatedMessageFoldsTapbackInRealTime(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Messages.SetMessages([]models.Message{
		{GUID: "message-a", Text: "hello", DateCreated: 1000},
	})
	app.windowManager.SetCachedMessages("chat-a", []models.Message{
		{GUID: "message-a", Text: "hello", DateCreated: 1000},
	})

	model, _ := app.Update(wsEventMsg(models.WSEvent{
		Type: "updated-message",
		Data: json.RawMessage(`{
			"guid": "reaction-a",
			"text": "Alice reacted with thumbs up",
			"dateCreated": 2000,
			"associatedMessageGuid": "message-a",
			"associatedMessageType": "like",
			"chatGuid": "chat-a"
		}`),
	}))
	app = *model.(*AppModel)

	// The reaction must render on the original message without adding a row.
	view := window.Messages.View()
	if !strings.Contains(view, "👍") {
		t.Fatalf("tapback emoji not rendered: %q", view)
	}
	if strings.Contains(view, "reacted with") {
		t.Fatalf("reaction prose should not render: %q", view)
	}
	if got, want := len(window.Messages.messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}

	// The cache must be folded too, so a later refetch keeps the reaction.
	cached := app.windowManager.GetCachedMessages("chat-a")
	if got, want := len(cached), 1; got != want {
		t.Fatalf("cached message count = %d, want %d", got, want)
	}
	if got, want := cached[0].ReactionCounts["👍"], 1; got != want {
		t.Fatalf("cached reaction count = %d, want %d", got, want)
	}
}

func TestAppModelUpdatedMessageSkipsNewMessageIndicators(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Messages.SetMessages([]models.Message{
		{GUID: "message-a", Text: "hello", DateCreated: 1000},
	})
	app.windowManager.SetCachedMessages("chat-a", []models.Message{
		{GUID: "message-a", Text: "hello", DateCreated: 1000},
	})

	model, _ := app.Update(wsEventMsg(models.WSEvent{
		Type: "updated-message",
		Data: json.RawMessage(`{
			"guid": "reaction-a",
			"text": "Alice reacted with thumbs up",
			"dateCreated": 2000,
			"associatedMessageGuid": "message-a",
			"associatedMessageType": "like",
			"chatGuid": "chat-a"
		}`),
	}))
	app = *model.(*AppModel)

	if got := app.chatList.NewMessageCount(); got != 0 {
		t.Fatalf("updated-message must not mark new messages, count = %d", got)
	}
}
