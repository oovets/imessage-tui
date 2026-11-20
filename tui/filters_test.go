package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// testBuffers names chats the way the IRC backend does, and gives everything
// else no name, as a backend without provider.BufferNamer.
func testBuffers(chatGUID string) string {
	if rest, ok := strings.CutPrefix(chatGUID, "irc:"); ok {
		return "irc." + strings.Replace(rest, ":", ".", 1)
	}
	return ""
}

func ircMessage(chat, nick, text string) models.Message {
	return models.Message{
		GUID:     "irc:libera:" + chat + ":" + nick + text,
		ChatGUID: "irc:libera:" + chat,
		Text:     text,
		Handle:   &models.Handle{Address: nick, DisplayName: nick},
	}
}

func TestFilterMatching(t *testing.T) {
	own := ircMessage("#go", "", "my own words")
	own.Handle, own.IsFromMe = nil, true
	imessage := models.Message{GUID: "x", ChatGUID: "chat-a", Text: "crypto", Handle: &models.Handle{DisplayName: "Anna"}}

	tests := []struct {
		name   string
		filter config.Filter
		msg    models.Message
		hidden bool
	}{
		{"every buffer, every line", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "*"},
			ircMessage("#go", "alice", "hi"), true},
		{"a buffer mask", config.Filter{Name: "f", Buffers: []string{"irc.libera.*"}, Tags: "*", Regex: "*"},
			ircMessage("#go", "alice", "hi"), true},
		{"another buffer", config.Filter{Name: "f", Buffers: []string{"irc.libera.#rust"}, Tags: "*", Regex: "*"},
			ircMessage("#go", "alice", "hi"), false},
		{"buffer names ignore case", config.Filter{Name: "f", Buffers: []string{"IRC.Libera.#GO"}, Tags: "*", Regex: "*"},
			ircMessage("#go", "alice", "hi"), true},
		{"an excluded buffer", config.Filter{Name: "f", Buffers: []string{"*", "!irc.libera.#go"}, Tags: "*", Regex: "*"},
			ircMessage("#go", "alice", "hi"), false},
		{"a chat with no buffer name is never filtered", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "*"},
			imessage, false},

		{"nick tag", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "nick_alice", Regex: "*"},
			ircMessage("#go", "alice", "hi"), true},
		{"nick tag for someone else", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "nick_alice", Regex: "*"},
			ircMessage("#go", "bob", "hi"), false},
		{"nick tag with a mask", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "nick_*bot*", Regex: "*"},
			ircMessage("#go", "GitBot42", "build passed"), true},
		{"tags are OR between commas", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "nick_alice,nick_bob", Regex: "*"},
			ircMessage("#go", "bob", "hi"), true},
		{"tags are AND with +", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "nick_bob+self_msg", Regex: "*"},
			ircMessage("#go", "bob", "hi"), false},
		{"a tag that must be missing", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "!nick_bob", Regex: "*"},
			ircMessage("#go", "alice", "hi"), true},
		{"own messages carry self_msg", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "self_msg", Regex: "*"},
			own, true},

		{"regex ignores case by default", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "crypto|airdrop"},
			ircMessage("#go", "spammer", "Free AIRDROP now"), true},
		{"(?-i) makes it case-sensitive", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "(?-i)^WeeChat sucks$"},
			ircMessage("#go", "x", "weechat sucks"), false},
		{"(?-i) still matches the exact case", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "(?-i)^WeeChat sucks$"},
			ircMessage("#go", "x", "WeeChat sucks"), true},
		{"! reverses the regex", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "!golang"},
			ircMessage("#go", "x", "off topic"), true},
		{"\\! is a literal !", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: `\!important`},
			ircMessage("#go", "x", "!important note"), true},
		{"\\t splits nick from message", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: `^bot\tdeploy`},
			ircMessage("#go", "bot", "deploy started"), true},
		{"the nick half must match too", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: `^bot\tdeploy`},
			ircMessage("#go", "alice", "deploy started"), false},
		{"a disabled filter hides nothing", config.Filter{Name: "f", Buffers: []string{"*"}, Tags: "*", Regex: "*", Disabled: true},
			ircMessage("#go", "alice", "hi"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMessageFilters(config.FiltersState{Filters: []config.Filter{tt.filter}}, testBuffers)
			if len(f.filters) != 1 {
				t.Fatalf("filter did not compile: %+v", tt.filter)
			}
			if got := f.hidden(tt.msg); got != tt.hidden {
				t.Fatalf("hidden = %v, want %v", got, tt.hidden)
			}
		})
	}
}

func TestIgnoreMatching(t *testing.T) {
	own := ircMessage("#go", "troll", "me, not the troll")
	own.IsFromMe = true

	tests := []struct {
		name   string
		ignore config.Ignore
		msg    models.Message
		hidden bool
	}{
		{"a nick everywhere", config.Ignore{Mask: "troll"}, ircMessage("#go", "Troll", "hi"), true},
		{"someone else", config.Ignore{Mask: "troll"}, ircMessage("#go", "alice", "hi"), false},
		{"a mask", config.Ignore{Mask: "troll*"}, ircMessage("#go", "troll_", "hi"), true},
		{"a regex", config.Ignore{Mask: "re:^tr[o0]ll$"}, ircMessage("#go", "tr0ll", "hi"), true},
		{"on its server", config.Ignore{Mask: "troll", Server: "libera"}, ircMessage("#go", "troll", "hi"), true},
		{"on another server", config.Ignore{Mask: "troll", Server: "oftc"}, ircMessage("#go", "troll", "hi"), false},
		{"in its channel", config.Ignore{Mask: "troll", Server: "libera", Channel: "#Go"}, ircMessage("#go", "troll", "hi"), true},
		{"in another channel", config.Ignore{Mask: "troll", Server: "libera", Channel: "#rust"}, ircMessage("#go", "troll", "hi"), false},
		{"never the user's own", config.Ignore{Mask: "troll"}, own, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMessageFilters(config.FiltersState{Ignores: []config.Ignore{tt.ignore}}, testBuffers)
			if got := f.hidden(tt.msg); got != tt.hidden {
				t.Fatalf("hidden = %v, want %v", got, tt.hidden)
			}
		})
	}
}

func TestTogglesTurnFiltersOffButNotIgnores(t *testing.T) {
	f := newMessageFilters(config.FiltersState{
		Filters: []config.Filter{{Name: "bots", Buffers: []string{"*"}, Tags: "nick_*bot", Regex: "*"}},
		Ignores: []config.Ignore{{Mask: "troll"}},
	}, testBuffers)
	bot := ircMessage("#go", "gitbot", "build passed")
	troll := ircMessage("#go", "troll", "hi")
	otherChannel := ircMessage("#rust", "gitbot", "build passed")

	mustRun := func(args, buffer string) {
		t.Helper()
		if _, err := f.runFilterCommand(args, buffer); err != nil {
			t.Fatal(err)
		}
	}

	mustRun("toggle", "")
	if f.hidden(bot) || !f.hidden(troll) {
		t.Fatalf("after alt+=: bot hidden %v, troll hidden %v; want the bot shown and the troll still ignored", f.hidden(bot), f.hidden(troll))
	}
	mustRun("toggle", "")

	mustRun("toggle @", "irc.libera.#go")
	if f.hidden(bot) {
		t.Fatal("filtering is off in #go, but the bot is hidden there")
	}
	if !f.hidden(otherChannel) {
		t.Fatal("filtering off in #go reached #rust")
	}
	mustRun("enable @", "irc.libera.#go")
	if !f.hidden(bot) {
		t.Fatal("filtering back on in #go, but the bot shows")
	}

	if _, err := f.runFilterCommand("toggle @", ""); err == nil {
		t.Fatal("@ in a chat with no buffer name succeeded")
	}
}

func TestFilterCommands(t *testing.T) {
	f := newMessageFilters(config.FiltersState{}, testBuffers)
	run := func(args string) (filterResult, error) {
		t.Helper()
		return f.runFilterCommand(args, "irc.libera.#go")
	}

	if _, err := run(" add spam irc.* * buy now|free  money"); err != nil {
		t.Fatal(err)
	}
	if got := f.state.Filters[0]; got.Name != "spam" || got.Regex != "buy now|free  money" || got.Buffers[0] != "irc.*" {
		t.Fatalf("stored %+v; the regex must keep its spaces", got)
	}
	if _, err := run(" add spam * * other"); err == nil {
		t.Fatal("add over an existing name succeeded")
	}
	if _, err := run(" addreplace spam * * other"); err != nil || f.state.Filters[0].Regex != "other" || len(f.filters) != 1 {
		t.Fatalf("addreplace: %v, %+v", err, f.state.Filters)
	}
	if _, err := run(" add broken * * ("); err == nil {
		t.Fatal("a regex that does not compile was added")
	}
	if _, err := run(" add incomplete *"); err == nil {
		t.Fatal("add without tags and regex succeeded")
	}

	if _, err := run(" disable spam"); err != nil || !f.state.Filters[0].Disabled {
		t.Fatalf("disable: %v, %+v", err, f.state.Filters[0])
	}
	if _, err := run(" toggle sp*"); err != nil || f.state.Filters[0].Disabled {
		t.Fatalf("toggle by mask: %v, %+v", err, f.state.Filters[0])
	}
	if _, err := run(" rename spam junk"); err != nil || f.state.Filters[0].Name != "junk" {
		t.Fatalf("rename: %v, %+v", err, f.state.Filters[0])
	}
	if _, err := run(" del nothing"); err == nil {
		t.Fatal("deleting a filter that is not there succeeded")
	}
	if _, err := run(" del junk"); err != nil || len(f.state.Filters) != 0 || len(f.filters) != 0 {
		t.Fatalf("del: %v, %+v", err, f.state.Filters)
	}

	if result, err := run(""); err != nil || !result.listing {
		t.Fatalf("/filter alone: %+v, %v; want the listing", result, err)
	}
	if _, err := run(" frobnicate"); err == nil {
		t.Fatal("an unknown action succeeded")
	}
}

func TestIgnoreCommands(t *testing.T) {
	f := newMessageFilters(config.FiltersState{}, testBuffers)
	for _, args := range []string{" add troll", " add re:^spam.* libera", " add pest libera #go"} {
		if _, err := f.runIgnoreCommand(args); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
	}
	if _, err := f.runIgnoreCommand(" add TROLL"); err == nil {
		t.Fatal("ignoring the same nick twice succeeded")
	}
	if _, err := f.runIgnoreCommand(" add someone!user@host"); err == nil {
		t.Fatal("a host mask, which nothing here can match, was accepted")
	}
	listing := strings.Join(f.listing(), "\n")
	for _, want := range []string{"1. troll everywhere", "2. re:^spam.* on libera", "3. pest in #go on libera"} {
		if !strings.Contains(listing, want) {
			t.Errorf("listing lacks %q:\n%s", want, listing)
		}
	}
	if _, err := f.runIgnoreCommand(" del 2"); err != nil || len(f.state.Ignores) != 2 || f.state.Ignores[1].Mask != "pest" {
		t.Fatalf("del 2: %v, %+v", err, f.state.Ignores)
	}
	if _, err := f.runIgnoreCommand(" del 9"); err == nil {
		t.Fatal("deleting an ignore that is not there succeeded")
	}
	if _, err := f.runIgnoreCommand(" del -all"); err != nil || len(f.state.Ignores) != 0 || len(f.ignores) != 0 {
		t.Fatalf("del -all: %v, %+v", err, f.state.Ignores)
	}
}

// bufferedProvider is a backend that names its chats like IRC does.
type bufferedProvider struct{ recordingProvider }

func (b *bufferedProvider) BufferName(chatGUID string) string { return testBuffers(chatGUID) }

var _ provider.BufferNamer = (*bufferedProvider)(nil)

// ircApp is the app with one IRC channel open in the focused pane, holding a
// line from a bot and one from a person.
func ircApp(t *testing.T) (AppModel, *ChatWindow) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	irc := &bufferedProvider{recordingProvider{id: "irc:libera"}}
	registry := provider.NewRegistry(&recordingProvider{id: "imessage"})
	registry.Register("irc:libera:", irc)
	app := NewAppModelWithConfig(registry, nil, nil)
	app = asApp(mustUpdate(app, tea.WindowSizeMsg{Width: 120, Height: 30}))

	channel := models.Chat{GUID: "irc:libera:#go", DisplayName: "#go"}
	app.chatList.SetChats([]models.Chat{channel})
	window := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(window, &channel)
	bot := ircMessage("#go", "gitbot", "build 123 passed")
	bot.DateCreated = 1
	person := ircMessage("#go", "alice", "anyone around?")
	person.DateCreated = 2
	window.Messages.SetMessages([]models.Message{bot, person})
	app.focused = focusWindow
	return app, window
}

func mustUpdate(app AppModel, msg tea.Msg) tea.Model {
	model, _ := app.Update(msg)
	return model
}

func typeAndSend(t *testing.T, app AppModel, window *ChatWindow, text string) AppModel {
	t.Helper()
	window.Input.textarea.SetValue(text)
	return asApp(mustUpdate(app, tea.KeyMsg{Type: tea.KeyEnter}))
}

func TestFilterCommandHidesLinesInThePane(t *testing.T) {
	app, window := ircApp(t)

	app = typeAndSend(t, app, window, "/filter add bots irc.* nick_*bot *")
	view := stripANSI(window.Messages.View())
	if strings.Contains(view, "build 123 passed") {
		t.Fatalf("the bot's line still shows:\n%s", view)
	}
	if !strings.Contains(view, "anyone around?") {
		t.Fatalf("the person's line is gone too:\n%s", view)
	}
	if header := stripANSI(window.paneHeaderLine(80)); !strings.Contains(header, "(1 filtered)") {
		t.Errorf("header %q does not say a line is filtered", header)
	}
	if got := window.Input.GetText(); got != "" {
		t.Errorf("composer kept %q after the command", got)
	}
	if got := window.Messages.LatestMessageGUID(); got != ircMessage("#go", "alice", "anyone around?").GUID {
		t.Errorf("a reaction would go to %q, not the latest line on screen", got)
	}

	// Saved, for the next start.
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "imessage-tui", "filters.json"))
	if err != nil || !strings.Contains(string(data), `"bots"`) {
		t.Fatalf("filters.json = %q, %v", data, err)
	}

	// alt+= shows everything again.
	app = asApp(mustUpdate(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}, Alt: true}))
	if view := stripANSI(window.Messages.View()); !strings.Contains(view, "build 123 passed") {
		t.Fatalf("alt+= did not bring the line back:\n%s", view)
	}
	app = asApp(mustUpdate(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}, Alt: true}))
	if view := stripANSI(window.Messages.View()); strings.Contains(view, "build 123 passed") {
		t.Fatalf("a second alt+= did not hide it again:\n%s", view)
	}

	// alt+- does the same for this buffer only.
	app = asApp(mustUpdate(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}, Alt: true}))
	if view := stripANSI(window.Messages.View()); !strings.Contains(view, "build 123 passed") {
		t.Fatalf("alt+- did not bring the line back:\n%s", view)
	}
	_ = app
}

func TestABadFilterCommandKeepsWhatWasTyped(t *testing.T) {
	app, window := ircApp(t)
	app = typeAndSend(t, app, window, "/filter add broken * * (")
	if got := window.Input.GetText(); got != "/filter add broken * * (" {
		t.Errorf("composer = %q; a typo should be left to fix", got)
	}
	if app.err == nil {
		t.Error("no error shown")
	}
}

func TestFilterListOpensAndAnyKeyCloses(t *testing.T) {
	app, window := ircApp(t)
	app = typeAndSend(t, app, window, "/ignore add troll")
	app = typeAndSend(t, app, window, "/filter list")
	if app.overlayLines == nil {
		t.Fatal("/filter list showed nothing")
	}
	if view := stripANSI(app.frame()); !strings.Contains(view, "1. troll everywhere") {
		t.Fatalf("listing lacks the ignore:\n%s", view)
	}
	app = asApp(mustUpdate(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}))
	if app.overlayLines != nil {
		t.Fatal("a key did not close the listing")
	}
}

func TestFilteredMessageDoesNotMarkUnreadOrNotify(t *testing.T) {
	app, window := ircApp(t)
	app = typeAndSend(t, app, window, "/ignore add troll")
	app.focused = focusChatList // so an ordinary message would mark the chat

	troll := ircMessage("#go", "troll", "bait")
	troll.DateCreated = 3
	app = asApp(mustUpdate(app, providerEventMsg(provider.Event{Kind: provider.EventNewMessage, ChatGUID: troll.ChatGUID, Message: troll})))
	if app.chatList.NewMessageCount() != 0 {
		t.Error("an ignored message marked the chat unread")
	}
	if window.Messages.HasUnseenIncoming() {
		t.Error("an ignored message marked the pane unseen")
	}
	if cached := app.windowManager.GetCachedMessages(troll.ChatGUID); len(cached) == 0 {
		t.Error("the ignored message was dropped from the cache; it should be kept, only hidden")
	}

	alice := ircMessage("#go", "alice", "still here")
	alice.DateCreated = 4
	app = asApp(mustUpdate(app, providerEventMsg(provider.Event{Kind: provider.EventNewMessage, ChatGUID: alice.ChatGUID, Message: alice})))
	if app.chatList.NewMessageCount() != 1 {
		t.Error("an ordinary message did not mark the chat unread; the check above proves nothing")
	}
}

func TestFiltersSurviveARestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.SaveFilters(config.FiltersState{
		Filters: []config.Filter{{Name: "bots", Buffers: []string{"irc.*"}, Tags: "nick_*bot", Regex: "*"}},
	}); err != nil {
		t.Fatal(err)
	}
	registry := provider.NewRegistry(nil)
	registry.Register("irc:libera:", &bufferedProvider{recordingProvider{id: "irc:libera"}})
	app := NewAppModelWithConfig(registry, nil, nil)
	if !app.filters.hidden(ircMessage("#go", "gitbot", "x")) {
		t.Fatal("a saved filter did not apply after a restart")
	}
}
