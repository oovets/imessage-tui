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

func TestTrimLinkToken(t *testing.T) {
	tests := []struct {
		field string
		want  string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com.", "https://example.com"},
		{"https://example.com/?!", "https://example.com/"},
		{"(https://example.com)", "https://example.com"},
		{"<https://example.com>", "https://example.com"},
		{"\"https://example.com\",", "https://example.com"},
		// Punctuation after a bracket used to survive, since the brackets
		// were trimmed before the full stop.
		{"https://youtu.be/abc).", "https://youtu.be/abc"},
		// A parenthesis the URL opened itself is part of it.
		{"https://en.wikipedia.org/wiki/Go_(game)", "https://en.wikipedia.org/wiki/Go_(game)"},
		{"(https://en.wikipedia.org/wiki/Go_(game)).", "https://en.wikipedia.org/wiki/Go_(game)"},
		{"...", ""},
	}
	for _, tt := range tests {
		if got := trimLinkToken(tt.field); got != tt.want {
			t.Errorf("trimLinkToken(%q) = %q, want %q", tt.field, got, tt.want)
		}
	}
}

func TestTextLinksOnlyReturnsHTTPLinks(t *testing.T) {
	text := "se https://a.example/x, file:///etc/passwd och HTTP://B.example/y " +
		"javascript://alert ftp://c.example http:/nohost https://a.example/x"
	want := []string{"https://a.example/x", "HTTP://B.example/y", "https://a.example/x"}
	if got := textLinks(text); !reflect.DeepEqual(got, want) {
		t.Errorf("textLinks = %q, want %q", got, want)
	}
}

// clickEveryCell clicks each cell of the window as drawn and returns, for each
// link that opened, the characters under the cells that opened it, in reading
// order. When the spans are right, that is exactly the URL as written.
func clickEveryCell(w *ChatWindow) map[string]string {
	got := make(map[string]string)
	for y, row := range strings.Split(ansi.Strip(w.View()), "\n") {
		x := 0
		for _, r := range row {
			if link, ok := w.LinkAt(x, y); ok {
				got[link] += string(r)
			}
			x += max(1, ansi.StringWidth(string(r)))
		}
	}
	return got
}

func TestClickOpensTheURLUnderThePointerOnly(t *testing.T) {
	// With colour on, the timestamp and sender name carry escape codes that
	// the URL search has to see past.
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	long := "https://example.com/" + strings.Repeat("abcdefghij", 8)
	tests := []struct {
		name string
		msg  models.Message
		bare bool // no line numbers or sender names
		want map[string]string
	}{
		{
			name: "incoming",
			msg:  models.Message{Text: "kolla https://example.com/a?b=1 nu", Handle: &models.Handle{DisplayName: "Alice"}},
			want: map[string]string{"https://example.com/a?b=1": "https://example.com/a?b=1"},
		},
		{
			// Outgoing lines are right-aligned, so their spans start after
			// the padding.
			name: "outgoing",
			msg:  models.Message{Text: "kolla https://example.com/a?b=1 nu", IsFromMe: true},
			want: map[string]string{"https://example.com/a?b=1": "https://example.com/a?b=1"},
		},
		{
			// Wider than the pane: hard-broken across rows, each piece
			// clickable, the continuation marker not.
			name: "wrapped",
			msg:  models.Message{Text: "lång " + long + " slut", Handle: &models.Handle{DisplayName: "Alice"}},
			want: map[string]string{long: long},
		},
		{
			name: "wrapped outgoing",
			msg:  models.Message{Text: long, IsFromMe: true},
			want: map[string]string{long: long},
		},
		{
			name: "two links and punctuation",
			msg:  models.Message{Text: "(se https://en.wikipedia.org/wiki/Go_(game)) och https://b.example.", Handle: &models.Handle{DisplayName: "Alice"}},
			want: map[string]string{
				"https://en.wikipedia.org/wiki/Go_(game)": "https://en.wikipedia.org/wiki/Go_(game)",
				"https://b.example":                       "https://b.example",
			},
		},
		{
			// Nothing before the URL but the coloured timestamp, whose
			// escape codes take no columns.
			name: "right after the timestamp",
			msg:  models.Message{Text: "https://example.com/a", Handle: &models.Handle{DisplayName: "Alice"}},
			bare: true,
			want: map[string]string{"https://example.com/a": "https://example.com/a"},
		},
		{
			// A bar takes two columns at the start of every row it is on,
			// and the URL's spans have to start after it.
			name: "beside a bar, wrapped",
			msg:  accented(models.Message{Text: "larm " + long + " slut", Handle: &models.Handle{DisplayName: "Alice"}}),
			want: map[string]string{long: long},
		},
		{
			name: "beside a bar, outgoing",
			msg:  accented(models.Message{Text: "kolla https://example.com/a?b=1 nu", IsFromMe: true}),
			want: map[string]string{"https://example.com/a?b=1": "https://example.com/a?b=1"},
		},
		{
			// The marks come out before the URL is looked for, so a bold
			// link opens without its asterisks.
			name: "bold link",
			msg:  models.Message{Text: "*https://example.com/a_b* ok", Format: models.FormatMrkdwn, Handle: &models.Handle{DisplayName: "Alice"}},
			want: map[string]string{"https://example.com/a_b": "https://example.com/a_b"},
		},
		{
			name: "not openable",
			msg:  models.Message{Text: "file:///etc/passwd", Handle: &models.Handle{DisplayName: "Alice"}},
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
			w := NewChatWindow(1)
			w.SetChat(&chat)
			w.SetBounds(0, 0, 40, 20)
			if tt.bare {
				w.Messages.SetShowLineNumbers(false)
				w.Messages.PinShowSenderNames(false)
			}
			msg := tt.msg
			msg.GUID = "m1"
			msg.DateCreated = 1_700_000_000_000
			w.Messages.SetMessages([]models.Message{msg})

			if got := clickEveryCell(w); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clicks opened %q, want %q\n%s", got, tt.want, ansi.Strip(w.View()))
			}
		})
	}
}

// A preview line has no URL left in its text, so it keeps opening its link
// from anywhere on the row.
func TestPreviewLineOpensFromAnywhere(t *testing.T) {
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	w := NewChatWindow(1)
	w.SetChat(&chat)
	w.SetBounds(0, 0, 40, 20)
	w.Messages.SetMessages([]models.Message{{
		GUID: "m1", Text: "https://youtu.be/abc", DateCreated: 1_700_000_000_000,
		Handle: &models.Handle{DisplayName: "Alice"},
	}})

	for y, row := range strings.Split(ansi.Strip(w.View()), "\n") {
		if !strings.Contains(row, "[YouTube]") {
			continue
		}
		for x := 1; x < len(row); x++ {
			if link, ok := w.LinkAt(x, y); !ok || link != "https://youtu.be/abc" {
				t.Fatalf("column %d of %q opened %q, %v", x, row, link, ok)
			}
		}
		return
	}
	t.Fatalf("no preview line drawn:\n%s", ansi.Strip(w.View()))
}

func TestParseOpenCommand(t *testing.T) {
	tests := []struct {
		raw     string
		msgNum  int
		linkNum int
		handled bool
		wantErr bool
	}{
		{raw: "/open", handled: true},
		{raw: "  /open  ", handled: true},
		{raw: "/open #3", msgNum: 3, linkNum: 1, handled: true},
		{raw: "/open 3", msgNum: 3, linkNum: 1, handled: true},
		{raw: "/open #3 2", msgNum: 3, linkNum: 2, handled: true},
		{raw: "/open #0", handled: true, wantErr: true},
		{raw: "/open #x", handled: true, wantErr: true},
		{raw: "/open #3 0", handled: true, wantErr: true},
		{raw: "/open #3 2 1", handled: true, wantErr: true},
		{raw: "/opener", handled: false},
		{raw: "open the door", handled: false},
		{raw: "/img #2", handled: false},
	}
	for _, tt := range tests {
		msgNum, linkNum, handled, err := parseOpenCommand(tt.raw)
		if handled != tt.handled {
			t.Errorf("%q: handled = %v, want %v", tt.raw, handled, tt.handled)
			continue
		}
		if tt.wantErr != (err != nil) {
			t.Errorf("%q: err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			continue
		}
		if err == nil && (msgNum != tt.msgNum || linkNum != tt.linkNum) {
			t.Errorf("%q: got #%d link %d, want #%d link %d", tt.raw, msgNum, linkNum, tt.msgNum, tt.linkNum)
		}
	}
}

func TestOpenCommandResolvesLinks(t *testing.T) {
	msgs := []models.Message{
		{GUID: "a", Text: "https://youtu.be/abc och https://example.com/one", DateCreated: 1000},
		{GUID: "b", Text: "ingen länk här", DateCreated: 2000},
		{GUID: "c", Text: "senast https://example.com/two", DateCreated: 3000},
		{GUID: "d", Text: "efteråt utan länk", DateCreated: 4000},
	}
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		// Screen order: the text's links, then the preview lines under it.
		{raw: "/open #1", want: "https://example.com/one"},
		{raw: "/open #1 2", want: "https://youtu.be/abc"},
		{raw: "/open #1 3", wantErr: true},
		{raw: "/open #2", wantErr: true},
		{raw: "/open #9", wantErr: true},
		{raw: "/open", want: "https://example.com/two"},
	}
	for _, tt := range tests {
		app := NewAppModelWithConfig(nil, nil, nil)
		chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
		window := app.windowManager.FocusedWindow()
		window.SetChat(&chat)
		window.Messages.SetMessages(append([]models.Message(nil), msgs...))

		msgNum, linkNum, _, err := parseOpenCommand(tt.raw)
		if err != nil {
			t.Fatalf("%q: %v", tt.raw, err)
		}
		got, err := linkToOpen(window, msgNum, linkNum)
		if tt.wantErr != (err != nil) {
			t.Errorf("%q: err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("%q opened %q, want %q", tt.raw, got, tt.want)
		}

		// Through the composer: a failure surfaces and opens nothing, and the
		// command is never sent as a message either way.
		window.Input.textarea.SetValue(tt.raw)
		cmd, handled := app.handleLocalInputCommand(window, tt.raw)
		if !handled {
			t.Errorf("%q: not handled", tt.raw)
		}
		if tt.wantErr {
			if cmd != nil || app.err == nil {
				t.Errorf("%q: cmd = %v, err = %v; want no command and an error", tt.raw, cmd != nil, app.err)
			}
			continue
		}
		if cmd == nil {
			t.Errorf("%q: no open command", tt.raw)
		}
		if window.Input.GetText() != "" {
			t.Errorf("%q: composer not cleared", tt.raw)
		}
	}
}

func TestOpenURLRefusesNonHTTPLinks(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript://x", "/usr/bin/xterm", "-flag"} {
		msg := openURLCmd(raw)()
		if _, ok := msg.(errMsg); !ok {
			t.Errorf("openURLCmd(%q) returned %T, want errMsg", raw, msg)
		}
	}
}
