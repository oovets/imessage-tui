package slack

import (
	"encoding/json"
	"testing"

	"github.com/oovets/imessage-tui/provider"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// newTestStream builds a stream without dialling anything: the pump is driven
// directly in these tests.
func newTestStream(t *testing.T, p *Provider) *Stream {
	t.Helper()
	return &Stream{
		provider:     p,
		events:       make(chan provider.Event, 8),
		reconnected:  make(chan struct{}, 4),
		disconnected: make(chan struct{}, 4),
		overflowed:   make(chan struct{}, 4),
	}
}

func TestStreamPublishesNewMessage(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "profile": map[string]any{"display_name": "Anna"}},
		}}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	stream := newTestStream(t, p)
	stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
		Type:      "message",
		Channel:   "C1",
		User:      "UANNA",
		Text:      "hej <!here>",
		TimeStamp: "1700000100.000100",
	}), nil)

	event := <-stream.Events()
	if event.Kind != provider.EventNewMessage {
		t.Errorf("kind = %v, want EventNewMessage", event.Kind)
	}
	if event.ChatGUID != "sl:acme:C1" {
		t.Errorf("chat guid = %q", event.ChatGUID)
	}
	if event.Message.GUID != "sl:acme:C1:1700000100.000100" {
		t.Errorf("message guid = %q", event.Message.GUID)
	}
	// The realtime path must run mrkdwn too, or a live message renders with
	// raw entities while the same message renders cleanly after a refresh.
	if event.Message.Text != "hej @here" {
		t.Errorf("text = %q, want mrkdwn converted", event.Message.Text)
	}
	if event.Message.Handle == nil || event.Message.Handle.DisplayName != "Anna" {
		t.Errorf("handle = %+v", event.Message.Handle)
	}
	if event.Message.ChatGUID != "sl:acme:C1" {
		t.Errorf("message chat guid = %q, want it set for the dedupe path", event.Message.ChatGUID)
	}
}

func TestStreamMarksOwnMessages(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	stream := newTestStream(t, p)
	stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
		Type: "message", Channel: "C1", User: "USELF", Text: "eget",
		TimeStamp: "1700000100.000100",
	}), nil)

	event := <-stream.Events()
	// Without this the echo of a message we just sent renders as somebody
	// else's, and the optimistic copy never reconciles.
	if !event.Message.IsFromMe {
		t.Error("own message not marked as from me")
	}
}

func TestStreamTreatsEditAsAnUpdate(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	stream := newTestStream(t, p)
	stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
		Type: "message", SubType: "message_changed", Channel: "C1",
		Message: &slackapi.Msg{
			User: "UANNA", Text: "rättad text", Timestamp: "1700000100.000100",
		},
	}), nil)

	event := <-stream.Events()
	// An edit must not mark the chat unread or reorder the list, which is what
	// the update kind means to the app.
	if event.Kind != provider.EventUpdatedMessage {
		t.Errorf("kind = %v, want EventUpdatedMessage", event.Kind)
	}
	if event.Message.Text != "rättad text" {
		t.Errorf("text = %q", event.Message.Text)
	}
	if event.Message.GUID != "sl:acme:C1:1700000100.000100" {
		t.Errorf("guid = %q, want the edited message's own guid", event.Message.GUID)
	}
}

func TestStreamIgnoresDeletions(t *testing.T) {
	fake := newFakeSlack(t)
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stream := newTestStream(t, p)
	stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
		Type: "message", SubType: "message_deleted", Channel: "C1",
		DeletedTimeStamp: "1700000100.000100",
	}), nil)

	select {
	case event := <-stream.Events():
		t.Fatalf("deletion published an event: %+v", event)
	default:
	}
}

// slackevents drops the files array, so an image posted live would arrive as an
// empty message if the raw envelope were not read back.
func TestStreamRecoversFilesFromTheRawEnvelope(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	payload := `{"event":{"type":"message","channel":"C1","user":"UANNA","ts":"1700000100.000100",
		"files":[{"id":"F9","name":"skiss.png","mimetype":"image/png",
		"url_private":"https://files.slack.com/skiss.png"}]}}`

	stream := newTestStream(t, p)
	stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
		Type: "message", Channel: "C1", User: "UANNA", TimeStamp: "1700000100.000100",
	}), &socketmode.Request{Payload: json.RawMessage(payload)})

	event := <-stream.Events()
	if len(event.Message.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1", len(event.Message.Attachments))
	}
	attachment := event.Message.Attachments[0]
	if attachment.GUID != "slfile:acme:F9" || attachment.SourceURL != "https://files.slack.com/skiss.png" {
		t.Errorf("attachment = %+v", attachment)
	}
}

func TestStreamRemembersLatestTimestampForMarkRead(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	fake.route("conversations.mark", func(map[string]string) any {
		return map[string]any{"ok": true}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	stream := newTestStream(t, p)
	stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
		Type: "message", Channel: "C1", User: "UANNA", Text: "live",
		TimeStamp: "1700000900.000100",
	}), nil)
	<-stream.Events()

	// A chat read purely through realtime must still be markable, or opening a
	// pane that never loaded history would leave Slack showing it as unread.
	if err := p.MarkRead("sl:acme:C1"); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if got := fake.formFor("conversations.mark")["ts"]; got != "1700000900.000100" {
		t.Errorf("marked read up to %q", got)
	}
}

func eventsAPI(inner *slackevents.MessageEvent) slackevents.EventsAPIEvent {
	return slackevents.EventsAPIEvent{
		Type:       slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{Type: "message", Data: inner},
	}
}
