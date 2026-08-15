package imessage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
	"github.com/oovets/imessage-tui/ws"
)

// BlueBubbles delivers the chat two different ways depending on the event, and
// getting it wrong drops the message on the floor: an empty ChatGUID makes the
// app ignore the event entirely.
func TestParseMessageEventFindsChatGUIDInEitherShape(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "chats array wins",
			data: `{"guid":"m1","text":"hi","chats":[{"guid":"chat-a"}],"chatGuid":"chat-b"}`,
			want: "chat-a",
		},
		{
			name: "flat chatGuid",
			data: `{"guid":"m1","text":"hi","chatGuid":"chat-b"}`,
			want: "chat-b",
		},
		{
			name: "neither",
			data: `{"guid":"m1","text":"hi"}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseMessageEvent(models.WSEvent{Type: "new-message", Data: json.RawMessage(tt.data)})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if msg.ChatGUID != tt.want {
				t.Errorf("ChatGUID = %q, want %q", msg.ChatGUID, tt.want)
			}
			if msg.GUID != "m1" {
				t.Errorf("GUID = %q, want m1", msg.GUID)
			}
		})
	}
}

func TestEventKindMapping(t *testing.T) {
	cases := map[string]provider.EventKind{
		"new-message":              provider.EventNewMessage,
		"updated-message":          provider.EventUpdatedMessage,
		"chat-read-status-changed": provider.EventUnknown,
		"typing-indicator":         provider.EventUnknown,
	}
	for wsType, want := range cases {
		if got := eventKind(wsType); got != want {
			t.Errorf("%s -> %v, want %v", wsType, got, want)
		}
	}
}

// The stream must keep publishing after a malformed payload. Dropping out of
// the loop would silently kill realtime for the rest of the session.
func TestStreamSurvivesUnparseableEvent(t *testing.T) {
	client := ws.NewClient("", "")
	stream := NewStream(client)
	go stream.translate()

	client.Events <- models.WSEvent{Type: "new-message", Data: json.RawMessage(`{ not json`)}
	client.Events <- models.WSEvent{
		Type: "new-message",
		Data: json.RawMessage(`{"guid":"m2","text":"after","chatGuid":"chat-a"}`),
	}
	close(client.Events)

	var got []provider.Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-stream.Events():
			if !ok {
				if len(got) != 2 {
					t.Fatalf("received %d events, want 2", len(got))
				}
				if got[0].Kind != provider.EventUnknown {
					t.Errorf("bad payload produced %v, want EventUnknown", got[0].Kind)
				}
				if got[1].Kind != provider.EventNewMessage || got[1].ChatGUID != "chat-a" {
					t.Errorf("second event = %+v, want a new message for chat-a", got[1])
				}
				return
			}
			got = append(got, event)
		case <-deadline:
			t.Fatalf("timed out after %d events", len(got))
		}
	}
}
