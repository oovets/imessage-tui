package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
)

// describe writes parsed lines back out with the styles spelled in brackets:
// [b:bold] [i:italic] [s:strike] [c:code], combined as [bi:both].
func describe(lines []mrkdwnLine) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		var sb strings.Builder
		if line.quote {
			sb.WriteString("> ")
		}
		for _, r := range line.runs {
			if r.style == 0 {
				sb.WriteString(r.text)
				continue
			}
			sb.WriteString("[")
			for _, f := range []struct {
				style markStyle
				name  string
			}{{markBold, "b"}, {markItalic, "i"}, {markStrike, "s"}, {markCode, "c"}} {
				if r.style&f.style != 0 {
					sb.WriteString(f.name)
				}
			}
			sb.WriteString(":" + r.text + "]")
		}
		out = append(out, sb.String())
	}
	return strings.Join(out, "\n")
}

func TestParseMrkdwn(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "the *raft leader* acts", "the [b:raft leader] acts"},
		{"italic, strike, code", "_snart_ ~gammalt~ `kod`", "[i:snart] [s:gammalt] [c:kod]"},
		{"nested", "*fet och _kursiv_*", "[b:fet och ][bi:kursiv]"},
		{"marks open and close at word edges only", "snake_case_name och 2*3*4", "snake_case_name och 2*3*4"},
		{"a mark followed by space is not one", "* inte fet *", "* inte fet *"},
		{"a letter before the mark keeps it literal", "å*b*", "å*b*"},
		{"punctuation around marks is fine", "(*fet*), _ja_!", "([b:fet]), [i:ja]!"},
		{"unclosed mark stays", "*halv", "*halv"},
		{"code is literal", "`*inte fet*` men *fet*", "[c:*inte fet*] men [b:fet]"},
		{
			// An underscore dropped from a URL opens a different page.
			name: "nothing inside a url is a mark",
			in:   "se https://x.example/a_b_c/*d* nu",
			want: "se https://x.example/a_b_c/*d* nu",
		},
		{"a bold url keeps its url", "*https://x.example/a_b*", "[b:https://x.example/a_b]"},
		{
			name: "a url inside a bold stretch",
			in:   "*se https://x.example/a_b nu*",
			want: "[b:se https://x.example/a_b nu]",
		},
		{"quote", "> citat *här*\nsvar", "> citat [b:här]\nsvar"},
		{"empty lines dropped", "a\n\n\nb", "a\nb"},
		{
			name: "preformatted block keeps its indentation and its marks",
			in:   "kör:\n```\nfunc *main*() {\n    return\n}\n```\nklart",
			want: "kör:\n[c:func *main*() {]\n[c:    return]\n[c:}]\nklart",
		},
		{"preformatted on one line", "kör ```ls -la``` nu", "kör [c:ls -la] nu"},
		{"an unclosed fence is text", "``` ingen kod", "``` ingen kod"},
		{"a third fence never closes", "```a``` och ```b", "[c:a] och ```b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describe(parseMrkdwn(tt.in)); got != tt.want {
				t.Errorf("parseMrkdwn(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPlainMrkdwnTakesTheMarksOut(t *testing.T) {
	got := plainMrkdwn("*Linear* closed `CORE-12`\n> citat\n```\nkod\n```")
	if want := "Linear closed CORE-12\n> citat\nkod"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// accented marks the whole message as one app attachment with a red bar.
func accented(msg models.Message) models.Message {
	msg.Format = models.FormatMrkdwn
	msg.Accents = []models.Accent{{Start: 0, End: len(msg.Text), Color: "#a30200"}}
	return msg
}

func TestPaneDrawsMarksAsStylesAndBarsBesideAttachments(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	body := "*Linear* closed an issue"
	card := "CORE-12 `Login` fails\n*Priority* Urgent"
	m := NewMessagesModel()
	m.SetSize(60, 10)
	m.SetShowTimestamps(false)
	m.SetShowLineNumbers(false)
	m.PinShowSenderNames(false)
	m.SetMessages([]models.Message{{
		GUID: "a", DateCreated: 1_700_000_000_000, Handle: &models.Handle{DisplayName: "Linear"},
		Text: body + "\n" + card, Format: models.FormatMrkdwn,
		Accents: []models.Accent{{Start: len(body) + 1, End: len(body) + 1 + len(card), Color: "#95a2b3"}},
	}})

	view := m.View()
	var rows []string
	for _, row := range strings.Split(ansi.Strip(view), "\n") {
		if row = strings.TrimRight(row, " "); row != "" && !strings.Contains(row, "──") {
			rows = append(rows, row)
		}
	}
	want := []string{"Linear closed an issue", "▎ CORE-12 Login fails", "▎ Priority Urgent"}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %q, want %q", rows, want)
	}
	for _, seq := range []string{
		"\x1b[1mLinear",           // bold
		"\x1b[38;2;149;162;179m▎", // the app's own colour
	} {
		if !strings.Contains(view, seq) {
			t.Errorf("view lacks %q:\n%q", seq, view)
		}
	}
}

// Accents written against one text must not cut another: a cached message
// whose text has since changed draws without bars rather than in pieces.
func TestAccentsThatDoNotFitAreIgnored(t *testing.T) {
	for _, accents := range [][]models.Accent{
		{{Start: 0, End: 99}},
		{{Start: 3, End: 2}},
		{{Start: 2, End: 4}, {Start: 1, End: 3}},
	} {
		segments := messageSegments(models.Message{Text: "hej då", Accents: accents})
		if len(segments) != 1 || segments[0].text != "hej då" || segments[0].bar != nil {
			t.Errorf("accents %+v gave segments %+v", accents, segments)
		}
	}
}

// /open lists what the reader sees, so a bold link opens without its marks.
func TestOpenListsLinksWithoutTheirMarks(t *testing.T) {
	m := NewMessagesModel()
	m.SetSize(60, 10)
	m.SetMessages([]models.Message{{
		GUID: "a", DateCreated: 1_700_000_000_000, Handle: &models.Handle{DisplayName: "Bot"},
		Text: "*https://x.example/a_b* och `https://y.example/c`", Format: models.FormatMrkdwn,
	}})
	if got, want := m.LinksByNumber(1), []string{"https://x.example/a_b", "https://y.example/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
}

// The format and accents describe one text; a merge that takes the text from
// one copy must take them from the same copy.
func TestMergeKeepsFormatWithItsText(t *testing.T) {
	withText := models.Message{GUID: "g", Text: "*larm*", Format: models.FormatMrkdwn,
		Accents: []models.Accent{{Start: 0, End: 6}}}
	empty := models.Message{GUID: "g", DateCreated: 1, Handle: &models.Handle{DisplayName: "Bot"}}

	for _, merged := range []models.Message{mergeDuplicateMessage(empty, withText), mergeDuplicateMessage(withText, empty)} {
		if merged.Text != withText.Text || merged.Format != withText.Format || !reflect.DeepEqual(merged.Accents, withText.Accents) {
			t.Errorf("merged = %+v", merged)
		}
	}
}
