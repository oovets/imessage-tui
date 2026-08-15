package slack

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	slackapi "github.com/slack-go/slack"
)

// fakeSlack is a Slack API stand-in. Routes are registered per test; anything
// unregistered fails the test loudly rather than returning a plausible empty
// answer, which would make a missing call look like a working one.
type fakeSlack struct {
	t      *testing.T
	server *httptest.Server

	mu     sync.Mutex
	routes map[string]func(form map[string]string) any
	calls  map[string]int
	forms  map[string][]map[string]string
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	fake := &fakeSlack{
		t:      t,
		routes: make(map[string]func(map[string]string) any),
		calls:  make(map[string]int),
		forms:  make(map[string][]map[string]string),
	}
	fake.route("auth.test", func(map[string]string) any {
		return map[string]any{"ok": true, "user_id": "USELF", "team": "Acme", "team_id": "T1"}
	})
	// Default lookup for ids that a test's users.list does not cover. Tests
	// that care about the lookup itself override this route.
	fake.route("users.info", func(form map[string]string) any {
		return map[string]any{"ok": true, "user": map[string]any{
			"id": form["user"], "name": form["user"],
		}}
	})
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeSlack) route(method string, handler func(form map[string]string) any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method] = handler
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/")
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("parse form for %s: %v", method, err)
	}
	form := make(map[string]string, len(r.Form))
	for key := range r.Form {
		form[key] = r.Form.Get(key)
	}

	f.mu.Lock()
	handler, ok := f.routes[method]
	f.calls[method]++
	f.forms[method] = append(f.forms[method], form)
	f.mu.Unlock()

	if !ok {
		f.t.Errorf("unexpected slack call: %s", method)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(handler(form)); err != nil {
		f.t.Errorf("encode %s: %v", method, err)
	}
}

func (f *fakeSlack) formFor(method string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	forms := f.forms[method]
	if len(forms) == 0 {
		f.t.Fatalf("%s was never called", method)
	}
	return forms[0]
}

func (f *fakeSlack) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func TestNewVerifiesTokenAndLearnsSelfID(t *testing.T) {
	fake := newFakeSlack(t)
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.SelfUserID() != "USELF" {
		t.Errorf("SelfUserID = %q, want USELF", p.SelfUserID())
	}
	if p.ID() != "slack:acme" {
		t.Errorf("ID = %q", p.ID())
	}
	if p.GUIDPrefix() != "sl:acme:" {
		t.Errorf("GUIDPrefix = %q", p.GUIDPrefix())
	}
}

func TestNewRejectsMissingToken(t *testing.T) {
	if _, err := New(Workspace{ID: "acme", Name: "Acme"}); err == nil {
		t.Fatal("New accepted a workspace with no token")
	}
	if _, err := New(Workspace{Name: "Acme", Token: "xoxp-test"}); err == nil {
		t.Fatal("New accepted a workspace with no id")
	}
}

// newAgainst is New with the API base pointed at the fake server.
func newAgainst(fake *fakeSlack, ws Workspace) (*Provider, error) {
	api := slackapi.New(ws.Token, slackapi.OptionAPIURL(fake.server.URL+"/"))
	auth, err := api.AuthTest()
	if err != nil {
		return nil, err
	}
	return newProvider(ws, api, auth.UserID), nil
}

func TestChatsNamesAndOrdersConversations(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "name": "anna", "real_name": "Anna Berg",
				"profile": map[string]any{"display_name": "Anna"}},
			map[string]any{"id": "UBOT", "name": "deploybot", "is_bot": true,
				"profile": map[string]any{"display_name": "Deploy Bot"}},
		}}
	})
	fake.route("users.conversations", func(map[string]string) any {
		return map[string]any{"ok": true, "channels": []any{
			map[string]any{"id": "CZED", "name": "zed", "is_channel": true},
			map[string]any{"id": "CGEN", "name": "general", "is_channel": true},
			map[string]any{"id": "DANNA", "is_im": true, "user": "UANNA"},
			map[string]any{"id": "DBOT", "is_im": true, "user": "UBOT"},
			map[string]any{"id": "GMPIM", "is_mpim": true, "name": "mpdm-anna--bo--carl-1"},
			map[string]any{"id": "CPRIV", "name": "hemligt", "is_private": true},
		}}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	chats, err := p.Chats(50)
	if err != nil {
		t.Fatalf("Chats: %v", err)
	}

	var got []string
	for _, chat := range chats {
		got = append(got, chat.DisplayName)
	}
	// People and group chats first, then channels; alphabetical inside each.
	// The bot DM is dropped: it is noise in a conversation list.
	want := []string{"Anna", "anna, bo, carl", "#general", "#hemligt", "#zed"}
	if len(got) != len(want) {
		t.Fatalf("chats = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chats = %v, want %v", got, want)
		}
	}

	if chats[0].GUID != "sl:acme:DANNA" {
		t.Errorf("dm guid = %q, want sl:acme:DANNA", chats[0].GUID)
	}
	if chats[0].ChatIdentifier != "DANNA" {
		t.Errorf("dm identifier = %q", chats[0].ChatIdentifier)
	}
}

func TestChatsFollowsPaginationCursor(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	fake.route("users.conversations", func(form map[string]string) any {
		if form["cursor"] == "" {
			return map[string]any{
				"ok":                true,
				"channels":          []any{map[string]any{"id": "C1", "name": "one", "is_channel": true}},
				"response_metadata": map[string]any{"next_cursor": "page2"},
			}
		}
		return map[string]any{
			"ok":       true,
			"channels": []any{map[string]any{"id": "C2", "name": "two", "is_channel": true}},
		}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	chats, err := p.Chats(500)
	if err != nil {
		t.Fatalf("Chats: %v", err)
	}
	if len(chats) != 2 {
		t.Fatalf("got %d chats, want 2 across both pages", len(chats))
	}
	if fake.callCount("users.conversations") != 2 {
		t.Errorf("users.conversations called %d times, want 2", fake.callCount("users.conversations"))
	}
}

func TestMessagesMapsHistory(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "name": "anna", "profile": map[string]any{"display_name": "Anna"}},
			map[string]any{"id": "USELF", "name": "stefan", "profile": map[string]any{"display_name": "Stefan"}},
		}}
	})
	// Slack returns history newest first.
	fake.route("conversations.history", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{
				"type": "message", "user": "USELF", "text": "svar från mig",
				"ts": "1700000200.000100",
			},
			map[string]any{
				"type": "message", "user": "UANNA", "text": "hej <@USELF> se <https://x.se|här>",
				"ts": "1700000100.000100",
				"reactions": []any{
					map[string]any{"name": "thumbsup", "count": 2, "users": []string{"UANNA", "USELF"}},
				},
				"files": []any{
					map[string]any{"id": "F1", "name": "bild.png", "mimetype": "image/png",
						"url_private": "https://files.slack.com/bild.png"},
				},
			},
		}}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	msgs, err := p.Messages("sl:acme:C1", 50)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}

	// Oldest first, whatever order Slack answered in.
	first, second := msgs[0], msgs[1]
	if first.DateCreated > second.DateCreated {
		t.Fatalf("messages not oldest-first: %d then %d", first.DateCreated, second.DateCreated)
	}

	if first.GUID != "sl:acme:C1:1700000100.000100" {
		t.Errorf("guid = %q", first.GUID)
	}
	if first.ChatGUID != "sl:acme:C1" {
		t.Errorf("chat guid = %q", first.ChatGUID)
	}
	if first.DateCreated != 1700000100000 {
		t.Errorf("dateCreated = %d, want 1700000100000", first.DateCreated)
	}
	if want := "hej @Stefan se här (https://x.se)"; first.Text != want {
		t.Errorf("text = %q, want %q", first.Text, want)
	}
	if first.IsFromMe {
		t.Error("message from UANNA marked as from me")
	}
	if first.Handle == nil || first.Handle.DisplayName != "Anna" {
		t.Errorf("handle = %+v, want Anna", first.Handle)
	}
	if got := first.ReactionCounts["👍"]; got != 2 {
		t.Errorf("reaction count = %d, want 2", got)
	}
	if len(first.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1", len(first.Attachments))
	}
	attachment := first.Attachments[0]
	if attachment.GUID != "slfile:acme:F1" || attachment.FileName != "bild.png" {
		t.Errorf("attachment = %+v", attachment)
	}
	if attachment.SourceURL != "https://files.slack.com/bild.png" {
		t.Errorf("attachment source url = %q", attachment.SourceURL)
	}
	// URL must stay empty: it is handed to the system opener, and Slack's
	// private url without the token is a login page.
	if attachment.URL != "" {
		t.Errorf("attachment url = %q, want empty", attachment.URL)
	}

	if !second.IsFromMe {
		t.Error("own message not marked as from me")
	}
	if second.Handle != nil {
		t.Errorf("own message carries a handle: %+v", second.Handle)
	}
}

func TestMessagesExpandsThreadsInline(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "profile": map[string]any{"display_name": "Anna"}},
		}}
	})
	fake.route("conversations.history", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "rot",
				"ts": "1700000100.000100", "reply_count": 1, "thread_ts": "1700000100.000100"},
		}}
	})
	fake.route("conversations.replies", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "rot",
				"ts": "1700000100.000100", "thread_ts": "1700000100.000100"},
			map[string]any{"type": "message", "user": "UANNA", "text": "svar i tråd",
				"ts": "1700000150.000100", "thread_ts": "1700000100.000100"},
		}}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs, err := p.Messages("sl:acme:C1", 50)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// The root must appear once, not twice: conversations.replies repeats it.
	if len(msgs) != 2 {
		var texts []string
		for _, m := range msgs {
			texts = append(texts, m.Text)
		}
		t.Fatalf("got %d messages %v, want root + reply", len(msgs), texts)
	}
	if msgs[0].Text != "rot" {
		t.Errorf("first message = %q, want the thread root", msgs[0].Text)
	}
	if msgs[1].Text != "↳ svar i tråd" {
		t.Errorf("reply = %q, want it marked as a thread reply", msgs[1].Text)
	}
}

func TestMessagesRejectsForeignWorkspace(t *testing.T) {
	fake := newFakeSlack(t)
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// With two workspaces connected, routing a call to the wrong one would
	// read — or post to — the wrong company.
	if _, err := p.Messages("sl:other:C1", 10); err == nil {
		t.Fatal("Messages accepted a chat from another workspace")
	}
	if err := p.Send("sl:other:C1", "hej", "", ""); err == nil {
		t.Fatal("Send accepted a chat from another workspace")
	}
	if _, err := p.Messages("chat-a", 10); err == nil {
		t.Fatal("Messages accepted an iMessage guid")
	}
}

func TestSendPostsToChannel(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("chat.postMessage", func(map[string]string) any {
		return map[string]any{"ok": true, "channel": "C1", "ts": "1700000300.000100"}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Send("sl:acme:C1", "hej där", "", "echo-guid"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	form := fake.formFor("chat.postMessage")
	if form["channel"] != "C1" {
		t.Errorf("channel = %q", form["channel"])
	}
	if form["text"] != "hej där" {
		t.Errorf("text = %q", form["text"])
	}
	if form["thread_ts"] != "" {
		t.Errorf("thread_ts = %q, want empty for a plain message", form["thread_ts"])
	}
}

func TestSendRepliesInThreadWhenGivenAMessageGUID(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("chat.postMessage", func(map[string]string) any {
		return map[string]any{"ok": true, "channel": "C1", "ts": "1700000300.000100"}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Send("sl:acme:C1", "svar", "sl:acme:C1:1700000100.000100", ""); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := fake.formFor("chat.postMessage")["thread_ts"]; got != "1700000100.000100" {
		t.Errorf("thread_ts = %q, want the replied-to timestamp", got)
	}
}

func TestReactMapsTapbacksToSlackEmoji(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("reactions.add", func(map[string]string) any {
		return map[string]any{"ok": true}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.React("sl:acme:C1", "sl:acme:C1:1700000100.000100", "like"); err != nil {
		t.Fatalf("React: %v", err)
	}
	form := fake.formFor("reactions.add")
	if form["name"] != "+1" {
		t.Errorf("emoji name = %q, want +1", form["name"])
	}
	if form["channel"] != "C1" || form["timestamp"] != "1700000100.000100" {
		t.Errorf("react addressed %q/%q", form["channel"], form["timestamp"])
	}

	if err := p.React("sl:acme:C1", "not-a-slack-message", "like"); err == nil {
		t.Error("React accepted a message guid it cannot address")
	}
}

func TestMarkReadUsesNewestSeenTimestamp(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	fake.route("conversations.history", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "nyast", "ts": "1700000200.000100"},
			map[string]any{"type": "message", "user": "UANNA", "text": "äldre", "ts": "1700000100.000100"},
		}}
	})
	fake.route("users.info", func(map[string]string) any {
		return map[string]any{"ok": true, "user": map[string]any{"id": "UANNA", "real_name": "Anna"}}
	})
	fake.route("conversations.mark", func(map[string]string) any {
		return map[string]any{"ok": true}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Nothing loaded yet: there is no timestamp to mark, and inventing one
	// would mark unread messages as read.
	if err := p.MarkRead("sl:acme:C1"); err != nil {
		t.Fatalf("MarkRead before history: %v", err)
	}
	if fake.callCount("conversations.mark") != 0 {
		t.Fatal("MarkRead called the API without knowing a timestamp")
	}

	if _, err := p.Messages("sl:acme:C1", 50); err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if err := p.MarkRead("sl:acme:C1"); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if got := fake.formFor("conversations.mark")["ts"]; got != "1700000200.000100" {
		t.Errorf("marked read up to %q, want the newest message", got)
	}
}

func TestUserCacheResolvesUnknownIDsOneAtATime(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "profile": map[string]any{"display_name": "Anna"}},
		}}
	})
	fake.route("users.info", func(map[string]string) any {
		return map[string]any{"ok": true, "user": map[string]any{
			"id": "ULATE", "real_name": "Sen Person",
		}}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.users.loadAll()

	if got := p.users.name("UANNA"); got != "Anna" {
		t.Errorf("cached name = %q", got)
	}
	if fake.callCount("users.info") != 0 {
		t.Error("a name already in the cache still cost a users.info call")
	}

	if got := p.users.name("ULATE"); got != "Sen Person" {
		t.Errorf("looked-up name = %q", got)
	}
	// The lookup must be remembered, or a busy channel would repeat it for
	// every message and burn the rate limit.
	if got := p.users.name("ULATE"); got != "Sen Person" {
		t.Errorf("second read = %q", got)
	}
	if got := fake.callCount("users.info"); got != 1 {
		t.Errorf("users.info called %d times, want 1", got)
	}
}

// The refresh loop reconciles every open pane on a timer. Re-expanding each
// thread every time would multiply one history call by ten, and
// conversations.replies allows about fifty a minute — so an unchanged thread
// must cost nothing.
func TestThreadsAreNotRefetchedUntilTheyChange(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})

	replyCount := 1
	fake.route("conversations.history", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "rot",
				"ts": "1700000100.000100", "reply_count": replyCount, "thread_ts": "1700000100.000100"},
		}}
	})
	fake.route("conversations.replies", func(map[string]string) any {
		messages := []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "rot",
				"ts": "1700000100.000100", "thread_ts": "1700000100.000100"},
			map[string]any{"type": "message", "user": "UANNA", "text": "svar 1",
				"ts": "1700000150.000100", "thread_ts": "1700000100.000100"},
		}
		if replyCount > 1 {
			messages = append(messages, map[string]any{
				"type": "message", "user": "UANNA", "text": "svar 2",
				"ts": "1700000160.000100", "thread_ts": "1700000100.000100",
			})
		}
		return map[string]any{"ok": true, "messages": messages}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for range 3 {
		if _, err := p.Messages("sl:acme:C1", 50); err != nil {
			t.Fatalf("Messages: %v", err)
		}
	}
	if got := fake.callCount("conversations.replies"); got != 1 {
		t.Errorf("thread fetched %d times across three unchanged refreshes, want 1", got)
	}

	// A new reply changes the count, which is the signal to refetch.
	replyCount = 2
	msgs, err := p.Messages("sl:acme:C1", 50)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if got := fake.callCount("conversations.replies"); got != 2 {
		t.Errorf("thread fetched %d times, want a refetch after the count changed", got)
	}
	if len(msgs) != 3 {
		t.Errorf("got %d messages, want root plus two replies", len(msgs))
	}
}

func TestChatNamesCarryTheWorkspaceWhenAsked(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "profile": map[string]any{"display_name": "Anna"}},
		}}
	})
	fake.route("users.conversations", func(map[string]string) any {
		return map[string]any{"ok": true, "channels": []any{
			map[string]any{"id": "CGEN", "name": "general", "is_channel": true},
			map[string]any{"id": "DANNA", "is_im": true, "user": "UANNA"},
		}}
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.ShowWorkspaceInNames(true)

	chats, err := p.Chats(50)
	if err != nil {
		t.Fatalf("Chats: %v", err)
	}
	got := []string{chats[0].DisplayName, chats[1].DisplayName}
	want := []string{"Acme Anna", "Acme #general"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chats = %v, want %v", got, want)
		}
	}
}

// The caller's chat limit means "the N most recent", which Slack's conversation
// list cannot answer — applying it returned whichever conversations Slack put
// first, which is how a workspace showed some channels and no DMs at all.
func TestChatsIgnoreTheCallersLimitAndPageThroughEverything(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{
			map[string]any{"id": "UANNA", "profile": map[string]any{"display_name": "Anna"}},
		}}
	})

	// Three pages, with the DM last — the shape that hid it.
	pages := map[string]any{
		"": map[string]any{
			"ok":                true,
			"channels":          channelsNamed("a", 60),
			"response_metadata": map[string]any{"next_cursor": "p2"},
		},
		"p2": map[string]any{
			"ok":                true,
			"channels":          channelsNamed("b", 60),
			"response_metadata": map[string]any{"next_cursor": "p3"},
		},
		"p3": map[string]any{
			"ok": true,
			"channels": []any{
				map[string]any{"id": "DANNA", "is_im": true, "user": "UANNA"},
			},
		},
	}
	fake.route("users.conversations", func(form map[string]string) any {
		return pages[form["cursor"]]
	})

	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 50 is the app's default chat limit, and what truncated the list.
	chats, err := p.Chats(50)
	if err != nil {
		t.Fatalf("Chats: %v", err)
	}
	if len(chats) != 121 {
		t.Fatalf("got %d chats, want all 121 across three pages", len(chats))
	}
	if chats[0].DisplayName != "Anna" {
		t.Errorf("first chat is %q, want the DM from the last page", chats[0].DisplayName)
	}
	if got := fake.callCount("users.conversations"); got != 3 {
		t.Errorf("fetched %d pages, want 3", got)
	}
}

func channelsNamed(prefix string, n int) []any {
	out := make([]any, 0, n)
	for i := range n {
		out = append(out, map[string]any{
			"id":         fmt.Sprintf("C%s%d", prefix, i),
			"name":       fmt.Sprintf("%s-%d", prefix, i),
			"is_channel": true,
		})
	}
	return out
}
