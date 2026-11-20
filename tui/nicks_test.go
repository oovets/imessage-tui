package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

func colourProfile(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

// ircSelf answers SelfNick the way the IRC backend does: a nick for its own
// chats, nothing for anyone else's.
func ircSelf(chatGUID string) string {
	if strings.HasPrefix(chatGUID, "irc:") {
		return "stefan"
	}
	return ""
}

func TestMentionsNick(t *testing.T) {
	tests := []struct {
		text, nick string
		want       bool
	}{
		{"stefan: are you there?", "stefan", true},
		{"ping Stefan", "stefan", true},
		{"thanks, stefan!", "stefan", true},
		{"stefans idea", "stefan", false},
		{"see https://github.com/stefan/repo", "stefan", false},
		{"\x1b[1mstefan\x1b[0m look", "stefan", true},
		{"[stefan] in brackets is a nick of its own", "stefan", false},
		{"a b c", "a", false},
		{"nick|away is back", "nick|away", true},
	}
	for _, tt := range tests {
		if got := mentionsNick(tt.text, tt.nick); got != tt.want {
			t.Errorf("mentionsNick(%q, %q) = %v, want %v", tt.text, tt.nick, got, tt.want)
		}
	}
}

func TestDecorateNicks(t *testing.T) {
	colourProfile(t)
	known := map[string]struct{}{"alice": {}, "bob": {}}

	line := "alice: ask bob or stefan, see https://x.com/alice"
	got := decorateNicks(line, known, "stefan", true, WideNickPalette)
	if ansi.Strip(got) != line {
		t.Fatalf("decorating changed the text: %q", ansi.Strip(got))
	}
	if code := colorBefore(got, "alice"); code == "" {
		t.Errorf("alice is not coloured: %q", got)
	}
	if code := colorBefore(got, "bob"); code == "" {
		t.Errorf("bob is not coloured: %q", got)
	}
	if !strings.Contains(got, "48;5;220mstefan") {
		t.Errorf("the user's nick is not highlighted: %q", got)
	}
	if !strings.Contains(got, "https://x.com/alice") {
		t.Errorf("the URL was broken up: %q", got)
	}
	if colorBefore(got, "carol") != "" {
		t.Error("a nick no one has used was coloured")
	}

	own := decorateNicks("stefan here", known, "stefan", false, WideNickPalette)
	if strings.Contains(own, "220m") {
		t.Errorf("the user's own message highlights their nick: %q", own)
	}
}

// ircPane is a pane on an IRC chat, holding messages from senders.
func ircPane(t *testing.T, messages ...models.Message) *MessagesModel {
	t.Helper()
	pane := NewMessagesModel()
	pane.SetSize(80, 20)
	pane.SetSelfNick(ircSelf)
	for i := range messages {
		messages[i].ChatGUID = "irc:libera:#go"
		if messages[i].DateCreated == 0 {
			messages[i].DateCreated = int64(1000 * (i + 1))
		}
		if messages[i].GUID == "" {
			messages[i].GUID = "g" + string(rune('a'+i))
		}
	}
	pane.SetMessages(messages)
	return &pane
}

func from(nick, text string) models.Message {
	return models.Message{Text: text, Handle: &models.Handle{Address: nick, DisplayName: nick}}
}

func TestIRCColoursANickFromTheFirstLine(t *testing.T) {
	colourProfile(t)

	// One person in a channel, and a query: both name and colour the nick,
	// where an iMessage chat with one sender would leave it plain.
	pane := ircPane(t, from("alice", "morning"))
	if colorBefore(pane.View(), "alice") == "" {
		t.Fatalf("a channel with one speaker left the nick plain:\n%s", pane.View())
	}

	plain := groupPane(t, "Anna")
	if colorBefore(plain.View(), "Anna") != "" {
		t.Fatal("an iMessage chat with one sender was coloured; only nick networks colour from the first line")
	}
}

func TestIRCUsesTheWidePalette(t *testing.T) {
	colourProfile(t)
	pane := ircPane(t, from("alice", "x"))
	want := string(nickStyleFrom("alice", WideNickPalette).GetForeground().(lipgloss.Color))
	if got := colorBefore(pane.View(), "alice"); got != want {
		t.Fatalf("alice is %s, want %s from the wide palette", got, want)
	}
}

func TestANickNamedInTheTextTakesItsOwnersColour(t *testing.T) {
	colourProfile(t)
	pane := ircPane(t, from("alice", "hello"), from("bob", "hi alice"))
	view := pane.View()
	label := colorBefore(view, "alice") // her own line comes first
	if label == "" {
		t.Fatalf("alice's own line is uncoloured:\n%s", view)
	}
	mention := colorBefore(view[strings.Index(view, "hi "):], "alice")
	if mention != label {
		t.Fatalf("alice is %s on her line and %s where bob names her", label, mention)
	}
}

func TestANewSpeakerRecoloursEarlierMentions(t *testing.T) {
	colourProfile(t)
	pane := ircPane(t, from("alice", "has anyone seen carol?"))
	if colorBefore(pane.View(), "carol") != "" {
		t.Fatal("carol was coloured before she said anything")
	}
	pane.AppendMessage(models.Message{GUID: "c1", ChatGUID: "irc:libera:#go", DateCreated: 9000,
		Text: "here", Handle: &models.Handle{DisplayName: "carol"}})
	view := pane.View()
	// The first carol on screen is alice's question, drawn before carol spoke.
	if colorBefore(view, "carol") == "" {
		t.Fatalf("the earlier mention of carol stayed plain once she spoke:\n%s", view)
	}
}

func TestAMentionOfTheUserIsHighlighted(t *testing.T) {
	colourProfile(t)
	pane := ircPane(t, from("alice", "stefan: lunch?"), from("bob", "nothing to see"))
	view := pane.View()
	line := view[:strings.Index(view, "lunch?")]
	if strings.Count(line, "48;5;220m") < 2 {
		t.Fatalf("want the sender and the nick in the text highlighted:\n%q", line)
	}
	other := view[strings.Index(view, "lunch?"):]
	if strings.Contains(other, "48;5;220m") {
		t.Fatalf("a line that does not name the user is highlighted:\n%q", other)
	}
}

func TestWidePalettesFollowThePaletteRules(t *testing.T) {
	for _, pair := range []struct{ narrow, wide []lipgloss.Color }{
		{nickPalette, nickPaletteWide},
		{nickPaletteDark, nickPaletteWideDark},
	} {
		if len(pair.wide) <= len(pair.narrow) {
			t.Errorf("the wide palette has %d colours, no more than the narrow one", len(pair.wide))
		}
		for i, c := range pair.narrow {
			if pair.wide[i] != c {
				t.Errorf("wide palette entry %d is %s, not the narrow palette's %s", i, pair.wide[i], c)
			}
		}
		for _, c := range pair.wide {
			if c == PaletteHighlight {
				t.Errorf("a nick could be drawn in the highlight colour %s", c)
			}
		}
	}
}

// nickProvider is an IRC-like backend: buffer names and a nick.
type nickProvider struct{ bufferedProvider }

func (n *nickProvider) SelfNick(chatGUID string) string { return ircSelf(chatGUID) }

var _ provider.NickNetwork = (*nickProvider)(nil)

func TestAMentionNotifiesThroughAMute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	registry := provider.NewRegistry(&recordingProvider{id: "imessage"})
	registry.Register("irc:libera:", &nickProvider{bufferedProvider{recordingProvider{id: "irc:libera"}}})
	app := NewAppModelWithConfig(registry, nil, nil)
	app.chatList.SetChats([]models.Chat{{GUID: "irc:libera:#go", DisplayName: "#go"}})
	app.showChatList = false
	app.focused = focusWindow
	app.notifyMode = notifyDesktop
	alerts := &[]notice{}
	app.alert = func(_ notifyMode, n notice) error {
		*alerts = append(*alerts, n)
		return nil
	}
	app = asApp(mustUpdate(app, tea.BlurMsg{}))
	app.chatList.ToggleMute("irc:libera:#go")

	quiet := models.Message{GUID: "q", ChatGUID: "irc:libera:#go", Text: "anyone?", DateCreated: 1,
		Handle: &models.Handle{DisplayName: "alice"}}
	app, toast := deliverMessage(t, app, "irc:libera:#go", quiet)
	if toast != "" || len(*alerts) != 0 {
		t.Fatalf("the muted channel announced an ordinary line: %q, %d", toast, len(*alerts))
	}

	mention := models.Message{GUID: "m", ChatGUID: "irc:libera:#go", Text: "stefan: you around?", DateCreated: 2,
		Handle: &models.Handle{DisplayName: "alice"}}
	_, toast = deliverMessage(t, app, "irc:libera:#go", mention)
	if want := "● #go · alice mentioned you: stefan: you around?"; toast != want {
		t.Errorf("toast = %q, want %q", toast, want)
	}
	if len(*alerts) != 1 {
		t.Fatalf("%d notifications, want the mention's", len(*alerts))
	}
}

func TestStripEmojisKeepsWhatNamesAreMadeOf(t *testing.T) {
	tests := map[string]string{
		"[k]":          "[k]",
		"foo_bar":      "foo_bar",
		"nick|away":    "nick|away",
		"^caret{x}\\`": "^caret{x}\\`",
		"#go":          "#go",
		"Familjen 🎉":   "Familjen",
		"👍🏽 Anna":      "Anna",
		"Åsa Ödmark":   "Åsa Ödmark",
		"a\x07b":       "ab",
	}
	for in, want := range tests {
		if got := stripEmojis(in); got != want {
			t.Errorf("stripEmojis(%q) = %q, want %q", in, got, want)
		}
	}
}

// A nick is its whole self: [k] is not k, and foo_bar is not foobar.
func TestDecoratedNicksShowWhole(t *testing.T) {
	pane := ircPane(t, from("[k]", "hej"), from("foo_bar", "tjena [k]"))
	view := ansi.Strip(pane.View())
	for _, want := range []string{"[k]: hej", "foo_bar: tjena [k]"} {
		if !strings.Contains(view, want) {
			t.Errorf("pane lacks %q:\n%s", want, view)
		}
	}
	if !mentionsNick("[k]: ping", "[k]") {
		t.Error("a mention of [k] was not recognised")
	}
}
