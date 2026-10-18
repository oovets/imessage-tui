package tui

import (
	"strings"
	"testing"

	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// tapbackEvent is the normalized form of the BlueBubbles "updated-message" a
// tapback arrives as. Turning the wire JSON into this is the provider's job and
// is tested there; what matters here is what the app does with it.
func tapbackEvent() providerEventMsg {
	return providerEventMsg(provider.Event{
		Kind:     provider.EventUpdatedMessage,
		ChatGUID: "chat-a",
		Message: models.Message{
			GUID:                  "reaction-a",
			Text:                  "Alice reacted with thumbs up",
			DateCreated:           2000,
			AssociatedMessageGUID: "message-a",
			AssociatedMessageType: "like",
			ChatGUID:              "chat-a",
		},
	})
}

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

	model, _ := app.Update(tapbackEvent())
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

	model, _ := app.Update(tapbackEvent())
	app = *model.(*AppModel)

	if got := app.chatList.NewMessageCount(); got != 0 {
		t.Fatalf("updated-message must not mark new messages, count = %d", got)
	}
}

// The confirmed copy of a sent message replaces the faint optimistic one as
// soon as the backend reports it, rather than both showing until the pending
// timeout. Telegram reports a send to a private chat only through the Send
// call itself, which is the case this pins from the UI's side.
func TestSentMessageEventReplacesPendingEcho(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "tg:100:u200", DisplayName: "Anna"}
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)

	pending := app.addPendingOutgoing(chat.GUID, "hej")
	if !app.hasPendingOutgoing(chat.GUID, pending.GUID) {
		t.Fatal("no optimistic copy")
	}

	app.Update(providerEventMsg(provider.Event{
		Kind:     provider.EventNewMessage,
		ChatGUID: chat.GUID,
		Message: models.Message{
			GUID:        "tg:100:u200:40",
			Text:        "hej",
			IsFromMe:    true,
			DateCreated: pending.DateCreated + 300,
			ChatGUID:    chat.GUID,
		},
	}))

	if app.hasPendingOutgoing(chat.GUID, pending.GUID) {
		t.Error("the optimistic copy is still pending after the confirmed message arrived")
	}
	view := stripANSI(window.Messages.renderedBody)
	if n := strings.Count(view, "hej"); n != 1 {
		t.Errorf("message drawn %d times, want once:\n%s", n, view)
	}
	if strings.Contains(view, "…") {
		t.Errorf("message still drawn as pending:\n%s", view)
	}
}
