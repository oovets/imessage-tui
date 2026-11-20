package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

func composer(text string) *InputModel {
	input := NewInputModel()
	input.Focus()
	input.textarea.SetValue(text)
	return &input
}

func TestCompleteNick(t *testing.T) {
	nicks := []string{"alice", "Adam", "bob"}
	tests := []struct {
		name    string
		text    string
		keys    []bool // one completion per entry; true is Shift+Tab
		want    string
		handled bool
	}{
		{"a nick that starts the message is addressed", "al", []bool{false}, "alice: ", true},
		{"one in the middle gets a space", "hej al", []bool{false}, "hej alice ", true},
		{"case is ignored, the nick's own case kept", "AD", []bool{false}, "Adam: ", true},
		{"Tab again moves to the next match", "a", []bool{false, false}, "Adam: ", true},
		{"and wraps around", "a", []bool{false, false, false}, "alice: ", true},
		{"Shift+Tab goes back", "a", []bool{false, false, true}, "alice: ", true},
		{"Shift+Tab first takes the last match", "a", []bool{true}, "Adam: ", true},
		{"no match leaves the word, but keeps the key", "zed", []bool{false}, "zed", true},
		{"no word, no completion", "hej ", []bool{false}, "hej ", false},
		{"an empty composer is not completed", "", []bool{false}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := composer(tt.text)
			handled := false
			for _, backward := range tt.keys {
				handled = input.CompleteNick(nicks, backward)
			}
			if got := input.GetText(); got != tt.want || handled != tt.handled {
				t.Fatalf("got %q handled %v, want %q handled %v", got, handled, tt.want, tt.handled)
			}
		})
	}
}

func TestTypingEndsACompletion(t *testing.T) {
	input := composer("a")
	input.CompleteNick([]string{"alice", "adam"}, false) // "alice: "
	*input, _ = input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi a")})
	input.CompleteNick([]string{"alice", "adam"}, false)
	if got := input.GetText(); got != "alice: hi alice " {
		t.Fatalf("got %q; Tab after typing must complete the new word, not cycle the old one", got)
	}
}

// nickListProvider is an IRC-like backend whose channels have members.
type nickListProvider struct {
	nickProvider
	members []string
}

func (n *nickListProvider) Nicks(string) []string { return n.members }

var _ provider.NickLister = (*nickListProvider)(nil)

func ircCompletionApp(t *testing.T) (AppModel, *ChatWindow) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	irc := &nickListProvider{
		nickProvider: nickProvider{bufferedProvider{recordingProvider{id: "irc:libera"}}},
		members:      []string{"aaron", "alba", "alex", "stefan"},
	}
	registry := provider.NewRegistry(&recordingProvider{id: "imessage"})
	registry.Register("irc:libera:", irc)
	app := NewAppModelWithConfig(registry, nil, nil)
	app = asApp(mustUpdate(app, tea.WindowSizeMsg{Width: 120, Height: 30}))

	channel := models.Chat{GUID: "irc:libera:#go", DisplayName: "#go"}
	app.chatList.SetChats([]models.Chat{channel, {GUID: "chat-a", DisplayName: "Family"}})
	window := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(window, &channel)
	older := ircMessage("#go", "alba", "first")
	older.DateCreated = 1
	newer := ircMessage("#go", "alex", "second")
	newer.DateCreated = 2
	window.Messages.SetMessages([]models.Message{older, newer})
	app.focused = focusWindow
	window.Input.Focus()
	return app, window
}

func pressTab(t *testing.T, app AppModel) AppModel {
	t.Helper()
	return asApp(mustUpdate(app, tea.KeyMsg{Type: tea.KeyTab}))
}

func TestTabCompletesNicksInSpeakerOrder(t *testing.T) {
	app, window := ircCompletionApp(t)
	window.Input.textarea.SetValue("a")

	var got []string
	for range 4 {
		app = pressTab(t, app)
		got = append(got, window.Input.GetText())
	}
	// alex spoke last, then alba; aaron is in the channel but silent; the
	// user's own nick is never offered.
	want := []string{"alex: ", "alba: ", "aaron: ", "alex: "}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tab presses gave %q, want %q", got, want)
		}
	}
	if app.focused != focusWindow {
		t.Fatal("completing moved focus away from the composer")
	}
}

func TestTabStillSwitchesFocusWhereThereIsNothingToComplete(t *testing.T) {
	app, window := ircCompletionApp(t)
	app.showChatList = true
	window.Input.textarea.SetValue("")
	app = pressTab(t, app)
	if app.focused != focusChatList {
		t.Fatal("Tab in an empty IRC composer no longer switches to the chat list")
	}

	// Outside IRC, Tab mid-word switches focus as it always has.
	app, window = ircCompletionApp(t)
	app.showChatList = true
	other := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.windowManager.ShowChat(window, &other)
	window.Input.Focus()
	window.Input.textarea.SetValue("al")
	app = pressTab(t, app)
	if app.focused != focusChatList || window.Input.GetText() != "al" {
		t.Fatalf("Tab in an iMessage chat completed or kept focus: %q", window.Input.GetText())
	}
}
