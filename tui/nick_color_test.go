package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
)

func groupPane(t *testing.T, senders ...string) *MessagesModel {
	t.Helper()
	pane := NewMessagesModel()
	pane.SetSize(60, 20)
	pane.SetShowSenderNames(true)

	messages := make([]models.Message, 0, len(senders))
	for i, sender := range senders {
		messages = append(messages, models.Message{
			GUID:        "m" + sender + string(rune('0'+i)),
			Text:        "hej från " + sender,
			DateCreated: int64(1000 + i*1000),
			Handle:      &models.Handle{DisplayName: sender},
		})
	}
	pane.SetMessages(messages)
	return &pane
}

// The colour is the whole point: it has to be per-person and the same person
// has to keep it, or it carries no information.
func TestGroupChatGivesEachSenderItsOwnColour(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	pane := groupPane(t, "Anna", "Bo", "Carl")
	view := pane.View()

	colors := map[string]string{}
	for _, name := range []string{"Anna", "Bo", "Carl"} {
		color := colorBefore(view, name)
		if color == "" {
			t.Fatalf("%s rendered without a colour: %q", name, view)
		}
		colors[name] = color
	}
	if colors["Anna"] == colors["Bo"] || colors["Bo"] == colors["Carl"] || colors["Anna"] == colors["Carl"] {
		t.Errorf("senders share colours: %v", colors)
	}
}

func TestSenderColourIsStableAcrossPanesAndOrder(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	first := groupPane(t, "Anna", "Bo").View()
	// Same people, different arrival order, a different pane: Anna must keep
	// her colour, or switching panes reshuffles who is who.
	second := groupPane(t, "Bo", "Anna").View()

	if a, b := colorBefore(first, "Anna"), colorBefore(second, "Anna"); a != b {
		t.Errorf("Anna is %s in one pane and %s in another", a, b)
	}
	// Case is not a person: "anna" and "Anna" are the same handle.
	mixed := groupPane(t, "anna", "Bo").View()
	if a, b := colorBefore(first, "Anna"), colorBefore(mixed, "anna"); a != b {
		t.Errorf("capitalisation changed the colour: %s vs %s", a, b)
	}
}

// A one-to-one chat has nothing to disambiguate, so the name stays plain.
func TestOneToOneChatDoesNotColourTheName(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	pane := groupPane(t, "Anna", "Anna")
	if got := colorBefore(pane.View(), "Anna"); got != "" {
		t.Errorf("single sender was coloured with %s", got)
	}
}

// The message that turns a chat into a group arrives through the incremental
// append path, which does not redraw earlier lines — so it has to fall back to
// a full render or the first sender stays uncoloured forever.
func TestNamesAlreadyOnScreenGetColouredWhenAGroupFormsLive(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	pane := groupPane(t, "Anna")
	if got := colorBefore(pane.View(), "Anna"); got != "" {
		t.Fatalf("one sender should not be coloured yet, got %s", got)
	}

	pane.AppendMessage(models.Message{
		GUID:        "m-bo",
		Text:        "hej",
		DateCreated: 5000,
		Handle:      &models.Handle{DisplayName: "Bo"},
	})

	view := pane.View()
	if got := colorBefore(view, "Anna"); got == "" {
		t.Errorf("the name already on screen was left uncoloured: %q", view)
	}
	if got := colorBefore(view, "Bo"); got == "" {
		t.Errorf("the new sender was left uncoloured: %q", view)
	}
}

func TestOwnNameIsNotColoured(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	pane := NewMessagesModel()
	pane.SetSize(60, 20)
	pane.SetShowSenderNames(true)
	pane.SetMessages([]models.Message{
		{GUID: "m1", Text: "hej", DateCreated: 1000, Handle: &models.Handle{DisplayName: "Anna"}},
		{GUID: "m2", Text: "hejsan", DateCreated: 2000, Handle: &models.Handle{DisplayName: "Bo"}},
		{GUID: "m3", Text: "svar", DateCreated: 3000, IsFromMe: true},
	})

	// Outgoing messages already carry the accent colour; a second colour on
	// "You" would compete with it.
	if got := colorBefore(pane.View(), "You"); got != "" {
		t.Errorf("own name coloured with %s", got)
	}
}

func TestNickPalettesAvoidTheColoursThatMeanSomethingElse(t *testing.T) {
	reserved := map[string]string{
		"32":  "the outgoing-message accent",
		"39":  "the outgoing-message accent on dark",
		"212": "the chat-list selection",
		"196": "the new-message marker",
		"243": "muted text",
		"242": "pane dividers",
	}
	for _, palette := range [][]lipgloss.Color{nickPalette, nickPaletteDark} {
		seen := map[string]struct{}{}
		for _, color := range palette {
			value := string(color)
			if what, clash := reserved[value]; clash {
				t.Errorf("nick palette reuses %s (%s)", value, what)
			}
			if _, dupe := seen[value]; dupe {
				t.Errorf("nick palette repeats %s, so two people can collide needlessly", value)
			}
			seen[value] = struct{}{}
		}
	}
}

var colorSeqRe = regexp.MustCompile(`\x1b\[38;5;(\d+)m$`)

// colorBefore returns the 256-colour code applied immediately before name, or
// "" when the name is rendered without one.
func colorBefore(view, name string) string {
	idx := strings.Index(view, name)
	if idx < 0 {
		return ""
	}
	match := colorSeqRe.FindStringSubmatch(view[:idx])
	if match == nil {
		return ""
	}
	return match[1]
}
