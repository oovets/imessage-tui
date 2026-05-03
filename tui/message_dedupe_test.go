package tui

import (
	"testing"

	"github.com/oovets/imessage-tui/models"
)

func TestMessagesModelSetMessagesDedupesByGUIDAndFingerprint(t *testing.T) {
	model := NewMessagesModel()
	model.SetMessages([]models.Message{
		{
			GUID:        "message-a",
			Text:        "same",
			IsFromMe:    false,
			DateCreated: 1000,
			ChatGUID:    "chat-a",
		},
		{
			GUID:        "message-a",
			Text:        "same",
			IsFromMe:    false,
			DateCreated: 1000,
			ChatGUID:    "chat-a",
			Attachments: []models.Attachment{
				{GUID: "attachment-a", MimeType: "image/png"},
			},
		},
		{
			GUID:        "message-a-alt",
			Text:        "same",
			IsFromMe:    false,
			DateCreated: 1000,
			ChatGUID:    "chat-a",
		},
		{
			GUID:        "message-b",
			Text:        "different",
			IsFromMe:    true,
			DateCreated: 2000,
			ChatGUID:    "chat-a",
		},
	})

	if got, want := len(model.messages), 2; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	if got, want := len(model.messages[0].Attachments), 1; got != want {
		t.Fatalf("merged attachment count = %d, want %d", got, want)
	}
}

func TestMessagesModelAppendMessageDedupesLiveVariant(t *testing.T) {
	model := NewMessagesModel()
	first := models.Message{
		GUID:        "message-a",
		Text:        "same",
		IsFromMe:    true,
		DateCreated: 1000,
		ChatGUID:    "chat-a",
	}
	second := first
	second.GUID = "message-a-alt"

	model.AppendMessage(first)
	model.AppendMessage(second)

	if got, want := len(model.messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
}

func TestWindowManagerCacheMessageDedupesLiveVariant(t *testing.T) {
	wm := NewWindowManager()
	first := models.Message{
		GUID:        "message-a",
		Text:        "same",
		IsFromMe:    false,
		DateCreated: 1000,
		ChatGUID:    "chat-a",
	}
	second := first
	second.GUID = "message-a-alt"

	if !wm.CacheMessage("chat-a", first) {
		t.Fatal("first cache insert was treated as duplicate")
	}
	if wm.CacheMessage("chat-a", second) {
		t.Fatal("duplicate live variant was inserted into cache")
	}
	if got, want := len(wm.GetCachedMessages("chat-a")), 1; got != want {
		t.Fatalf("cached message count = %d, want %d", got, want)
	}
}
