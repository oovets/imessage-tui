package slack

import (
	"testing"

	"github.com/oovets/imessage-tui/provider"
	"github.com/slack-go/slack/slackevents"
)

const (
	threadRootTS  = "1700000100.000100"
	threadReplyTS = "1700000150.000100"
	threadGUID    = "sl:acme:C1:" + threadRootTS
)

// threadFixture is a workspace with one channel, #general, holding one thread:
// a root by Anna and a reply by Bo.
func threadFixture(t *testing.T) (*fakeSlack, *Provider) {
	t.Helper()
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "profile": map[string]any{"display_name": "Anna"}},
			map[string]any{"id": "UBO", "profile": map[string]any{"display_name": "Bo"}},
		}}
	})
	fake.route("users.conversations", func(map[string]string) any {
		return map[string]any{"ok": true, "channels": []any{
			map[string]any{"id": "C1", "name": "general", "is_channel": true},
		}}
	})
	fake.route("conversations.history", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "rot",
				"ts": threadRootTS, "reply_count": 1, "thread_ts": threadRootTS},
		}}
	})
	fake.route("conversations.replies", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "rot",
				"ts": threadRootTS, "thread_ts": threadRootTS, "reply_count": 1},
			map[string]any{"type": "message", "user": "UBO", "text": "svar i tråd",
				"ts": threadReplyTS, "thread_ts": threadRootTS},
		}}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Chats(50); err != nil {
		t.Fatalf("Chats: %v", err)
	}
	if _, err := p.Messages("sl:acme:C1", 50); err != nil {
		t.Fatalf("Messages: %v", err)
	}
	return fake, p
}

func TestThreadIsTheSameConversationFromAnyOfItsMessages(t *testing.T) {
	_, p := threadFixture(t)
	for name, messageGUID := range map[string]string{
		"from the root":         "sl:acme:C1:" + threadRootTS,
		"from a reply":          "sl:acme:C1:" + threadReplyTS,
		"from the thread's own": threadGUID,
	} {
		t.Run(name, func(t *testing.T) {
			thread, err := p.Thread(messageGUID)
			if err != nil {
				t.Fatalf("Thread: %v", err)
			}
			if thread.GUID != threadGUID {
				t.Errorf("guid = %q, want %q", thread.GUID, threadGUID)
			}
			if thread.DisplayName != "#general › thread" {
				t.Errorf("name = %q, want the channel's name", thread.DisplayName)
			}
		})
	}
}

func TestThreadRejectsWhatIsNotAMessageHere(t *testing.T) {
	_, p := threadFixture(t)
	for _, guid := range []string{"sl:acme:C1", "sl:other:C1:" + threadRootTS, "chat-a"} {
		if _, err := p.Thread(guid); err == nil {
			t.Errorf("Thread(%q) succeeded", guid)
		}
	}
}

func TestMessagesForAThreadReadTheThreadUnmarked(t *testing.T) {
	fake, p := threadFixture(t)
	historyCalls := fake.callCount("conversations.history")

	msgs, err := p.Messages(threadGUID, 50)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if fake.callCount("conversations.history") != historyCalls {
		t.Error("a thread pane fetched the channel's history")
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want root + reply", len(msgs))
	}
	// In the thread's own pane every message is in the thread; the channel's
	// reply marker would only be noise.
	if msgs[0].Text != "rot" || msgs[1].Text != "svar i tråd" {
		t.Errorf("texts = %q, %q, want them without the reply marker", msgs[0].Text, msgs[1].Text)
	}
	for _, msg := range msgs {
		if msg.ChatGUID != threadGUID {
			t.Errorf("chat guid = %q, want the thread's", msg.ChatGUID)
		}
	}
}

func TestMessagesForALongThreadKeepTheRootAndTheNewestReplies(t *testing.T) {
	fake, p := threadFixture(t)
	fake.route("conversations.replies", func(map[string]string) any {
		messages := []any{map[string]any{"type": "message", "user": "UANNA", "text": "rot",
			"ts": threadRootTS, "thread_ts": threadRootTS}}
		for _, ts := range []string{"1700000201.000100", "1700000202.000100", "1700000203.000100"} {
			messages = append(messages, map[string]any{"type": "message", "user": "UBO",
				"text": ts, "ts": ts, "thread_ts": threadRootTS})
		}
		return map[string]any{"ok": true, "messages": messages}
	})

	msgs, err := p.Messages(threadGUID, 3)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	var texts []string
	for _, msg := range msgs {
		texts = append(texts, msg.Text)
	}
	want := []string{"rot", "1700000202.000100", "1700000203.000100"}
	if len(texts) != len(want) || texts[0] != want[0] || texts[1] != want[1] || texts[2] != want[2] {
		t.Errorf("texts = %q, want %q", texts, want)
	}
}

func TestSendToAThreadPostsInIt(t *testing.T) {
	fake, p := threadFixture(t)
	fake.route("chat.postMessage", func(map[string]string) any {
		return map[string]any{"ok": true, "channel": "C1", "ts": "1700000300.000100"}
	})
	if err := p.Send(threadGUID, "hej", "", ""); err != nil {
		t.Fatalf("Send: %v", err)
	}
	form := fake.formFor("chat.postMessage")
	if form["channel"] != "C1" {
		t.Errorf("channel = %q, want C1", form["channel"])
	}
	if form["thread_ts"] != threadRootTS {
		t.Errorf("thread_ts = %q, want the thread root", form["thread_ts"])
	}
}

func TestMarkReadLeavesAThreadAlone(t *testing.T) {
	fake, p := threadFixture(t)
	// conversations.mark is not routed: the fake fails the test if it is
	// called at all.
	if err := p.MarkRead(threadGUID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if n := fake.callCount("conversations.mark"); n != 0 {
		t.Errorf("conversations.mark called %d times for a thread", n)
	}
}

func TestStreamPublishesAReplyToAnOpenThreadThereToo(t *testing.T) {
	_, p := threadFixture(t)
	reply := func(stream *Stream, ts string) {
		stream.handleEventsAPI(eventsAPI(&slackevents.MessageEvent{
			Type:            "message",
			Channel:         "C1",
			User:            "UBO",
			Text:            "nytt svar",
			TimeStamp:       ts,
			ThreadTimeStamp: threadRootTS,
		}), nil)
	}

	stream := newTestStream(t, p)
	reply(stream, "1700000400.000100")
	if got := drainEvents(stream); len(got) != 1 || got[0].ChatGUID != "sl:acme:C1" {
		t.Fatalf("before the thread is open: events %+v, want one for the channel only", got)
	}

	if _, err := p.Messages(threadGUID, 50); err != nil {
		t.Fatalf("Messages: %v", err)
	}
	reply(stream, "1700000500.000100")
	got := drainEvents(stream)
	if len(got) != 2 {
		t.Fatalf("got %d events, want one for the channel and one for the thread", len(got))
	}
	if got[0].ChatGUID != "sl:acme:C1" || got[0].Message.Text != "↳ nytt svar" {
		t.Errorf("channel event = %q %q", got[0].ChatGUID, got[0].Message.Text)
	}
	if got[1].ChatGUID != threadGUID || got[1].Message.ChatGUID != threadGUID || got[1].Message.Text != "nytt svar" {
		t.Errorf("thread event = %q %q %q, want the thread's, unmarked",
			got[1].ChatGUID, got[1].Message.ChatGUID, got[1].Message.Text)
	}
}

func drainEvents(stream *Stream) []provider.Event {
	var events []provider.Event
	for {
		select {
		case event := <-stream.Events():
			events = append(events, event)
		default:
			return events
		}
	}
}
