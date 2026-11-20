package irc

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// fakeWire records what the provider sends and can answer it, the way a
// server's reply comes back through the read loop.
type fakeWire struct {
	mu       sync.Mutex
	nick     string
	caps     map[string]string
	isupport map[string]string
	sent     []ircmsg.Message
	// answer runs for every line sent, outside the lock.
	answer func(ircmsg.Message)
}

func (w *fakeWire) SendWithTags(tags map[string]string, command string, params ...string) error {
	msg := ircmsg.MakeMessage(tags, "", command, params...)
	w.mu.Lock()
	w.sent = append(w.sent, msg)
	answer := w.answer
	w.mu.Unlock()
	if answer != nil {
		answer(msg)
	}
	return nil
}

func (w *fakeWire) CurrentNick() string                 { return w.nick }
func (w *fakeWire) AcknowledgedCaps() map[string]string { return w.caps }
func (w *fakeWire) ISupport() map[string]string         { return w.isupport }
func (w *fakeWire) MaxMessageLength(string) int         { return 400 }
func (w *fakeWire) Quit()                               {}

// sentCommand returns every line sent with command.
func (w *fakeWire) sentCommand(command string) []ircmsg.Message {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []ircmsg.Message
	for _, msg := range w.sent {
		if msg.Command == command {
			out = append(out, msg)
		}
	}
	return out
}

var fullCaps = []string{"server-time", "message-tags", "echo-message", "batch", "draft/chathistory", "draft/read-marker"}

// newTestProvider is a provider registered on a fake connection with caps.
func newTestProvider(t *testing.T, caps ...string) (*Provider, *fakeWire) {
	t.Helper()
	w := &fakeWire{
		nick:     "me",
		caps:     map[string]string{},
		isupport: map[string]string{"CASEMAPPING": "rfc1459", "CHANTYPES": "#&", "STATUSMSG": "@+"},
	}
	for _, c := range caps {
		w.caps[c] = ""
	}
	p := newProvider(Network{ID: "libera", Name: "Libera", Channels: []string{"#go"}}, w)
	p.limiter = nil
	p.settle = 0
	// A server with chathistory answers every request, if only with an
	// empty batch; one left unanswered would wait out historyTimeout.
	answerHistory(t, p, w, nil, nil)
	p.onRegistered()
	return p, w
}

func parse(t *testing.T, raw string) ircmsg.Message {
	t.Helper()
	msg, err := ircmsg.ParseLine(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return msg
}

func batchOf(t *testing.T, params []string, lines ...string) *ircevent.Batch {
	t.Helper()
	batch := &ircevent.Batch{Message: ircmsg.MakeMessage(nil, "bouncer", "BATCH", params...)}
	for _, raw := range lines {
		batch.Items = append(batch.Items, &ircevent.Batch{Message: parse(t, raw)})
	}
	return batch
}

// events drains what the stream has published so far.
func events(p *Provider) []provider.Event {
	var out []provider.Event
	for {
		select {
		case e := <-p.stream.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestGUIDsRoundTrip(t *testing.T) {
	chat := ChatGUID("libera", "#go")
	if network, target, ok := ParseChatGUID(chat); !ok || network != "libera" || target != "#go" {
		t.Fatalf("ParseChatGUID(%q) = %q, %q, %v", chat, network, target, ok)
	}
	// A msgid may hold anything but a space, colons included.
	msg := MessageGUID("libera", "#go", "a:b:c")
	if network, target, msgid, ok := ParseMessageGUID(msg); !ok || network != "libera" || target != "#go" || msgid != "a:b:c" {
		t.Fatalf("ParseMessageGUID(%q) = %q, %q, %q, %v", msg, network, target, msgid, ok)
	}
	if _, _, ok := ParseChatGUID(msg); ok {
		t.Fatalf("a message GUID parsed as a chat GUID")
	}
	if _, _, _, ok := ParseMessageGUID("sl:acme:C1:123"); ok {
		t.Fatalf("a Slack GUID parsed as an IRC one")
	}
}

func TestCaseFolding(t *testing.T) {
	tests := []struct {
		mapping caseMapping
		in, out string
	}{
		{mapRFC1459, "#Go[Lang]", "#go{lang}"},
		{mapRFC1459, `Nick\~`, "nick|^"},
		{mapRFC1459Strict, `Nick\~`, "nick|~"},
		{mapASCII, "#Go[Lang]", "#go[lang]"},
		{mapRFC1459, "#Åsa", "#Åsa"},
	}
	for _, tt := range tests {
		if got := tt.mapping.fold(tt.in); got != tt.out {
			t.Errorf("fold(%q) under %d = %q, want %q", tt.in, tt.mapping, got, tt.out)
		}
	}
}

func TestConvertText(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		text   string
		format models.TextFormat
	}{
		{"plain keeps its asterisks", "2*3*4 is *not* bold", "2*3*4 is *not* bold", ""},
		{"bold", "\x02hi\x02 there", "*hi* there", models.FormatMrkdwn},
		{"spaces stay outside the mark", "say \x02 loud \x02now", "say  *loud* now", models.FormatMrkdwn},
		{"italic inside bold", "\x02\x1dboth\x0f", "*_both_*", models.FormatMrkdwn},
		{"monospace", "run \x11go test\x11", "run `go test`", models.FormatMrkdwn},
		{"colour has no mark and is dropped", "\x0304,01red\x03 text", "red text", ""},
		{"underline has no mark and is dropped", "\x1funder\x1f", "under", ""},
		{"a bell never reaches the terminal", "ding\x07dong\ttab", "dingdong tab", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, format := convertText(tt.in)
			if text != tt.text || format != tt.format {
				t.Fatalf("convertText(%q) = %q, %q; want %q, %q", tt.in, text, format, tt.text, tt.format)
			}
		})
	}
}

func TestSplitForWire(t *testing.T) {
	long := strings.Repeat("word ", 30) // 150 bytes
	tests := []struct {
		name  string
		text  string
		max   int
		check func(t *testing.T, pieces []string)
	}{
		{"one line", "hello", 100, func(t *testing.T, pieces []string) {
			if len(pieces) != 1 || pieces[0] != "hello" {
				t.Fatalf("pieces = %q", pieces)
			}
		}},
		{"newlines split, blank lines go", "one\r\n\ntwo\n", 100, func(t *testing.T, pieces []string) {
			if strings.Join(pieces, "|") != "one|two" {
				t.Fatalf("pieces = %q", pieces)
			}
		}},
		{"long line cuts at a space and rejoins exactly", long, 64, func(t *testing.T, pieces []string) {
			if strings.Join(pieces, "") != long {
				t.Fatalf("pieces do not rejoin to the line: %q", pieces)
			}
			for _, piece := range pieces {
				if len(piece) > 64 {
					t.Fatalf("piece of %d bytes over the limit: %q", len(piece), piece)
				}
				if !strings.HasSuffix(piece, " ") && piece != pieces[len(pieces)-1] {
					t.Fatalf("piece cut inside a word: %q", piece)
				}
			}
		}},
		{"one long word cuts on a rune boundary", strings.Repeat("å", 40), 21, func(t *testing.T, pieces []string) {
			for _, piece := range pieces {
				if !strings.HasPrefix(strings.Repeat("å", 40), piece) && !strings.Contains(strings.Repeat("å", 40), piece) {
					t.Fatalf("piece split a rune: %q", piece)
				}
				if len(piece)%2 != 0 {
					t.Fatalf("piece split a rune: %q", piece)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.check(t, splitForWire(tt.text, tt.max)) })
	}
}

func TestIncomingMessages(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		chatGUID string
		text     string
		fromMe   bool
		sender   string
	}{
		{
			name:     "channel message",
			line:     "@msgid=abc;time=2026-11-20T10:00:00.000Z :alice!a@host PRIVMSG #Go :hello",
			chatGUID: "irc:libera:#go",
			text:     "hello",
			sender:   "alice",
		},
		{
			name:     "a message to the channel's operators shows in the channel",
			line:     "@msgid=abc :alice!a@host PRIVMSG @#go :ops only",
			chatGUID: "irc:libera:#go",
			text:     "ops only",
			sender:   "alice",
		},
		{
			name:     "private message is filed under its sender",
			line:     "@msgid=abc :Bob!b@host PRIVMSG me :hi you",
			chatGUID: "irc:libera:bob",
			text:     "hi you",
			sender:   "Bob",
		},
		{
			name:     "own echo of a private message is filed under its recipient",
			line:     "@msgid=abc :me!m@host PRIVMSG Bob :hi back",
			chatGUID: "irc:libera:bob",
			text:     "hi back",
			fromMe:   true,
		},
		{
			name:     "action",
			line:     "@msgid=abc :alice!a@host PRIVMSG #go :\x01ACTION waves\x01",
			chatGUID: "irc:libera:#go",
			text:     "* alice waves",
			sender:   "alice",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestProvider(t, fullCaps...)
			p.handle(parse(t, tt.line))
			got := events(p)
			if len(got) != 1 {
				t.Fatalf("published %d events, want 1", len(got))
			}
			msg := got[0].Message
			if got[0].Kind != provider.EventNewMessage || got[0].ChatGUID != tt.chatGUID || msg.ChatGUID != tt.chatGUID {
				t.Fatalf("event %+v, want a new message in %s", got[0], tt.chatGUID)
			}
			if msg.GUID != tt.chatGUID+":abc" {
				t.Errorf("GUID = %q, want %q", msg.GUID, tt.chatGUID+":abc")
			}
			if msg.Text != tt.text || msg.IsFromMe != tt.fromMe {
				t.Errorf("text %q fromMe %v, want %q %v", msg.Text, msg.IsFromMe, tt.text, tt.fromMe)
			}
			switch {
			case tt.sender == "" && msg.Handle != nil:
				t.Errorf("own message carries a handle: %+v", msg.Handle)
			case tt.sender != "" && (msg.Handle == nil || msg.Handle.DisplayName != tt.sender):
				t.Errorf("handle = %+v, want %s", msg.Handle, tt.sender)
			}
		})
	}
}

func TestServerTimeIsTheMessageTime(t *testing.T) {
	p, _ := newTestProvider(t, fullCaps...)
	p.handle(parse(t, "@msgid=abc;time=2026-11-20T10:00:00.250Z :alice!a@host PRIVMSG #go :hello"))
	want := time.Date(2026, 11, 20, 10, 0, 0, 250e6, time.UTC).UnixMilli()
	if got := events(p)[0].Message.DateCreated; got != want {
		t.Fatalf("DateCreated = %d, want %d", got, want)
	}
}

func TestLinesNoOneReadsAreDropped(t *testing.T) {
	for _, raw := range []string{
		":irc.example.net NOTICE * :*** Looking up your hostname",
		":alice!a@host PRIVMSG me :\x01VERSION\x01",
		":alice!a@host PRIVMSG #go :\x03\x02\x0f",
	} {
		p, _ := newTestProvider(t, fullCaps...)
		p.handle(parse(t, raw))
		if got := events(p); len(got) != 0 {
			t.Errorf("%q published %+v", raw, got)
		}
	}
}

func TestMessageWithoutMsgidGetsAStableID(t *testing.T) {
	p, _ := newTestProvider(t)
	line := "@time=2026-11-20T10:00:00.000Z :alice!a@host PRIVMSG #go :hello"
	p.handle(parse(t, line))
	p.handle(parse(t, line))
	got := events(p)
	if len(got) != 1 {
		t.Fatalf("the same line twice published %d events, want 1", len(got))
	}
	_, _, msgid, _ := ParseMessageGUID(got[0].Message.GUID)
	if !isSynthetic(msgid) {
		t.Fatalf("msgid %q is not marked as made up here", msgid)
	}
}

func TestRepliesNameTheMessageTheyAnswer(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	p.handle(parse(t, "@msgid=r1;+draft/reply=orig :alice!a@host PRIVMSG #go :yes"))
	if got := events(p)[0].Message.ReplyToGUID; got != "irc:libera:#go:orig" {
		t.Fatalf("ReplyToGUID = %q", got)
	}

	if err := p.Send("irc:libera:#go", "agreed", "irc:libera:#go:orig", ""); err != nil {
		t.Fatal(err)
	}
	sent := w.sentCommand("PRIVMSG")
	if len(sent) != 1 {
		t.Fatalf("sent %d PRIVMSGs", len(sent))
	}
	if _, reply := sent[0].GetTag("+draft/reply"); reply != "orig" {
		t.Fatalf("reply tag = %q, want orig", reply)
	}
}

// answerHistory makes the fake answer CHATHISTORY the way a bouncer does, and
// counts the LATEST requests per target.
func answerHistory(t *testing.T, p *Provider, w *fakeWire, history map[string][]string, targets []string) map[string]int {
	t.Helper()
	asked := make(map[string]int)
	var mu sync.Mutex
	w.answer = func(msg ircmsg.Message) {
		if msg.Command != "CHATHISTORY" {
			return
		}
		switch msg.Params[0] {
		case "LATEST":
			target := msg.Params[1]
			mu.Lock()
			asked[target]++
			mu.Unlock()
			p.onBatch(batchOf(t, []string{"+h", "chathistory", target}, history[target]...))
		case "TARGETS":
			var lines []string
			for _, target := range targets {
				lines = append(lines, ":bouncer CHATHISTORY TARGETS "+target)
			}
			p.onBatch(batchOf(t, []string{"+t", "draft/chathistory-targets"}, lines...))
		}
	}
	return asked
}

func TestHistoryIsAskedForOncePerConnection(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	asked := answerHistory(t, p, w, map[string][]string{
		"#go": {
			"@msgid=1;time=2026-11-20T10:00:00.000Z :alice!a@host PRIVMSG #go :first",
			"@msgid=2;time=2026-11-20T10:01:00.000Z :bob!b@host PRIVMSG #go :second",
		},
	}, nil)

	for i := 0; i < 3; i++ {
		msgs, err := p.Messages("irc:libera:#go", 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 2 || msgs[0].Text != "first" || msgs[1].Text != "second" {
			t.Fatalf("read %d: messages = %+v", i, msgs)
		}
	}
	if asked["#go"] != 1 {
		t.Fatalf("history asked for %d times across three reads, want 1", asked["#go"])
	}
	if got := events(p); len(got) != 0 {
		t.Fatalf("history published %d events; it is not news", len(got))
	}

	// A reconnect: what was said meanwhile is only in the server's history.
	p.onRegistered()
	select {
	case <-p.stream.reconnected:
	default:
		t.Fatal("a second registration did not signal a reconnect")
	}
	if _, err := p.Messages("irc:libera:#go", 50); err != nil {
		t.Fatal(err)
	}
	if asked["#go"] != 2 {
		t.Fatalf("history asked for %d times after a reconnect, want 2", asked["#go"])
	}
}

func TestWithoutChathistoryMessagesReadsWhatArrived(t *testing.T) {
	p, w := newTestProvider(t, "server-time", "message-tags")
	p.handle(parse(t, "@msgid=1 :alice!a@host PRIVMSG #go :live"))
	msgs, err := p.Messages("irc:libera:#go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "live" {
		t.Fatalf("messages = %+v", msgs)
	}
	if got := w.sentCommand("CHATHISTORY"); len(got) != 0 {
		t.Fatalf("asked a server without chathistory for history: %+v", got)
	}
}

func TestChatsListsChannelsAndConversations(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	answerHistory(t, p, w, nil, []string{
		"alice timestamp=2026-11-20T12:00:00.000Z",
		"#Rust timestamp=2026-11-20T11:00:00.000Z",
	})
	p.handle(parse(t, ":me!m@host JOIN #Rust"))
	p.handle(parse(t, ":me!m@host JOIN #go"))
	p.handle(parse(t, ":me!m@host JOIN #old"))
	p.handle(parse(t, ":me!m@host PART #old :bye"))
	p.handle(parse(t, ":stranger!s@host JOIN #elsewhere"))

	chats, err := p.Chats(0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, chat := range chats {
		got = append(got, chat.GUID+"="+chat.DisplayName)
	}
	want := []string{"irc:libera:alice=alice", "irc:libera:#rust=#Rust", "irc:libera:#go=#go"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("chats = %v\nwant    %v", got, want)
	}
	for _, chat := range chats {
		if len(chat.Participants) != 0 {
			// A channel's member list would become its name in the list.
			t.Errorf("%s carries participants: %+v", chat.GUID, chat.Participants)
		}
	}
}

func TestTwoNetworksLabelTheirChats(t *testing.T) {
	p, _ := newTestProvider(t, fullCaps...)
	p.ShowNetworkInNames(true)
	chats, err := p.Chats(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].DisplayName != "Libera #go" || chats[0].ShortName != "#go" {
		t.Fatalf("chats = %+v", chats)
	}
	if got := p.Label(); got != "IRC · Libera" {
		t.Fatalf("Label = %q", got)
	}
}

func TestMultiLineSendComesBackAsOneMessage(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	text := "first line\nsecond line"
	if err := p.Send("irc:libera:#go", text, "", "local-echo"); err != nil {
		t.Fatal(err)
	}
	if sent := w.sentCommand("PRIVMSG"); len(sent) != 2 {
		t.Fatalf("sent %d lines, want 2", len(sent))
	}
	if got := events(p); len(got) != 0 {
		t.Fatalf("published before the server echoed: %+v", got)
	}

	p.handle(parse(t, "@msgid=e1;time=2026-11-20T10:00:00.000Z :me!m@host PRIVMSG #go :first line"))
	if got := events(p); len(got) != 0 {
		t.Fatalf("published after half the echoes: %+v", got)
	}
	p.handle(parse(t, "@msgid=e2;time=2026-11-20T10:00:00.001Z :me!m@host PRIVMSG #go :second line"))
	got := events(p)
	if len(got) != 1 {
		t.Fatalf("published %d events, want the whole message once", len(got))
	}
	if msg := got[0].Message; msg.Text != text || !msg.IsFromMe || msg.GUID != "irc:libera:#go:e1" {
		t.Fatalf("message = %+v", msg)
	}

	// After a reconnect, history brings the two lines back separately.
	answerHistory(t, p, w, map[string][]string{"#go": {
		"@msgid=e1;time=2026-11-20T10:00:00.000Z :me!m@host PRIVMSG #go :first line",
		"@msgid=e2;time=2026-11-20T10:00:00.001Z :me!m@host PRIVMSG #go :second line",
	}}, nil)
	p.onRegistered()
	msgs, err := p.Messages("irc:libera:#go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != text {
		t.Fatalf("after history: %+v", msgs)
	}
}

func TestSendWithoutEchoShowsTheUIsOwnCopy(t *testing.T) {
	p, w := newTestProvider(t, "server-time")
	if err := p.Send("irc:libera:#go", "hello", "", "local-echo"); err != nil {
		t.Fatal(err)
	}
	if sent := w.sentCommand("PRIVMSG"); len(sent) != 1 || sent[0].Params[0] != "#go" || sent[0].Params[1] != "hello" {
		t.Fatalf("sent %+v", sent)
	}
	got := events(p)
	if len(got) != 1 || got[0].Message.GUID != "local-echo" || !got[0].Message.IsFromMe {
		t.Fatalf("events = %+v; want the message under the UI's echo GUID", got)
	}
}

func TestReactions(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	p.handle(parse(t, "@msgid=m1 :alice!a@host PRIVMSG #go :ship it"))
	events(p)

	react := "@msgid=x1;+draft/react=👍;+draft/reply=m1 :bob!b@host TAGMSG #go"
	p.handle(parse(t, react))
	p.handle(parse(t, react)) // the same reaction again, as history would bring it
	msgs, err := p.Messages("irc:libera:#go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := msgs[0].ReactionCounts["👍"]; got != 1 {
		t.Fatalf("👍 counted %d times, want 1", got)
	}

	if err := p.React("irc:libera:#go", "irc:libera:#go:m1", "love"); err != nil {
		t.Fatal(err)
	}
	sent := w.sentCommand("TAGMSG")
	if len(sent) != 1 || sent[0].Params[0] != "#go" {
		t.Fatalf("sent %+v", sent)
	}
	if _, emoji := sent[0].GetTag("+draft/react"); emoji != "❤️" {
		t.Errorf("react tag = %q", emoji)
	}
	if _, reply := sent[0].GetTag("+draft/reply"); reply != "m1" {
		t.Errorf("reply tag = %q", reply)
	}
	// The map handed out earlier must not change under the caller.
	if len(msgs[0].ReactionCounts) != 1 {
		t.Fatalf("a reaction wrote into a map Messages already returned")
	}
}

func TestReactNeedsMessageTags(t *testing.T) {
	p, w := newTestProvider(t, "server-time")
	p.handle(parse(t, "@msgid=m1 :alice!a@host PRIVMSG #go :ship it"))
	if err := p.React("irc:libera:#go", "irc:libera:#go:m1", "like"); err == nil {
		t.Fatal("React succeeded on a server without message-tags")
	}
	if got := w.sentCommand("TAGMSG"); len(got) != 0 {
		t.Fatalf("sent %+v", got)
	}
}

func TestReadMarkers(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	p.handle(parse(t, "@msgid=1;time=2026-11-20T10:00:00.000Z :alice!a@host PRIVMSG #go :one"))
	p.handle(parse(t, "@msgid=2;time=2026-11-20T10:01:00.000Z :alice!a@host PRIVMSG #go :two"))
	p.handle(parse(t, ":bouncer MARKREAD #go timestamp=2026-11-20T10:00:00.000Z"))

	chats, err := p.Chats(0)
	if err != nil {
		t.Fatal(err)
	}
	if chats[0].UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1 after the marker", chats[0].UnreadCount)
	}

	if err := p.MarkRead("irc:libera:#go"); err != nil {
		t.Fatal(err)
	}
	sent := w.sentCommand("MARKREAD")
	if len(sent) != 1 || sent[0].Params[0] != "#go" || sent[0].Params[1] != "timestamp=2026-11-20T10:01:00.000Z" {
		t.Fatalf("sent %+v", sent)
	}
	if err := p.MarkRead("irc:libera:#go"); err != nil {
		t.Fatal(err)
	}
	if got := w.sentCommand("MARKREAD"); len(got) != 1 {
		t.Fatalf("marking read twice sent %d markers, want 1", len(got))
	}
}

func TestGUIDsOfOtherNetworksAreRefused(t *testing.T) {
	p, _ := newTestProvider(t, fullCaps...)
	if _, err := p.Messages("irc:oftc:#go", 50); err == nil {
		t.Fatal("read a chat of another network")
	}
	if err := p.Send("sl:acme:C1", "hi", "", ""); err == nil {
		t.Fatal("sent to a Slack chat")
	}
}

func TestJoinLinesPackChannels(t *testing.T) {
	lines := joinLines([]string{"#a", " #b ", "", "#c"})
	if len(lines) != 1 || lines[0] != "#a,#b,#c" {
		t.Fatalf("lines = %q", lines)
	}
	if err := (&fakeWire{}).SendWithTags(nil, "JOIN", lines[0]); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationJoinsConfiguredChannels(t *testing.T) {
	_, w := newTestProvider(t, fullCaps...)
	joins := w.sentCommand("JOIN")
	if len(joins) != 1 || joins[0].Params[0] != "#go" {
		t.Fatalf("joins = %+v", joins)
	}
}

func TestLimiterLetsABurstThroughThenSpacesLines(t *testing.T) {
	l := &limiter{burst: 3, every: 50 * time.Millisecond}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.wait(start.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 30*time.Millisecond {
		t.Fatalf("the burst waited %v", elapsed)
	}
	if err := l.wait(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("the line after the burst went after %v", elapsed)
	}
	if err := l.wait(time.Now()); err == nil {
		t.Fatal("a line that cannot go before its deadline was let through")
	}
}

func TestBufferNamesAreWeeChats(t *testing.T) {
	p, _ := newTestProvider(t, fullCaps...)
	tests := map[string]string{
		"irc:libera:#go":  "irc.libera.#go",
		"irc:libera:bob":  "irc.libera.bob",
		"irc:oftc:#go":    "",
		"sl:acme:C1":      "",
		"not-a-guid-here": "",
	}
	for guid, want := range tests {
		if got := p.BufferName(guid); got != want {
			t.Errorf("BufferName(%q) = %q, want %q", guid, got, want)
		}
	}

	registry := provider.NewRegistry(nil)
	registry.Register(p.GUIDPrefix(), p)
	if got := registry.BufferName("irc:libera:#go"); got != "irc.libera.#go" {
		t.Errorf("registry.BufferName = %q", got)
	}
	if got := registry.BufferName("chat-a"); got != "" {
		t.Errorf("a chat no backend names got %q", got)
	}
}

func TestSelfNickIsTheNickTheServerKnows(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	if got := p.SelfNick("irc:libera:#go"); got != "me" {
		t.Errorf("SelfNick = %q, want me", got)
	}
	w.nick = "me_" // the server gave another nick, e.g. after a collision
	if got := p.SelfNick("irc:libera:#go"); got != "me_" {
		t.Errorf("after a nick change SelfNick = %q, want me_", got)
	}
	if got := p.SelfNick("irc:oftc:#go"); got != "" {
		t.Errorf("another network's chat got %q", got)
	}
	registry := provider.NewRegistry(nil)
	registry.Register(p.GUIDPrefix(), p)
	if got := registry.SelfNick("irc:libera:bob"); got != "me_" {
		t.Errorf("registry.SelfNick = %q", got)
	}
	if got := registry.SelfNick("chat-a"); got != "" {
		t.Errorf("a chat on no nick network got %q", got)
	}
}

func TestNicksFollowTheChannel(t *testing.T) {
	p, w := newTestProvider(t, fullCaps...)
	w.isupport["PREFIX"] = "(qov)~@+"
	p.onRegistered()

	for _, raw := range []string{
		":me!m@host JOIN #go",
		":server 353 me = #go :~me @Alice +bob carol!c@host",
		":server 353 me = #go :@+dave",
		":server 366 me #go :End of /NAMES list",
		":erin!e@host JOIN #go",
		":carol!c@host PART #go :bye",
		":Alice!a@host NICK alice_away",
		":dave!d@host QUIT :gone",
		":op!o@host KICK #go bob :spam",
	} {
		p.handle(parse(t, raw))
	}
	got := strings.Join(p.Nicks("irc:libera:#go"), " ")
	if want := "alice_away erin"; got != want {
		t.Fatalf("Nicks = %q, want %q", got, want)
	}

	// A new NAMES replaces the list whole, rather than adding to it.
	p.handle(parse(t, ":server 353 me = #go :me frank"))
	p.handle(parse(t, ":server 366 me #go :End of /NAMES list"))
	if got := strings.Join(p.Nicks("irc:libera:#go"), " "); got != "frank" {
		t.Fatalf("after a second NAMES: %q", got)
	}

	p.handle(parse(t, "@msgid=q :Grace!g@host PRIVMSG me :hi"))
	if got := p.Nicks("irc:libera:grace"); len(got) != 1 || got[0] != "Grace" {
		t.Fatalf("a query's nicks = %q, want the other person", got)
	}
	if got := p.Nicks("irc:oftc:#go"); got != nil {
		t.Fatalf("another network's chat got %q", got)
	}
}
