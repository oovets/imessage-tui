package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

func forceColor(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

var (
	quoteErik = &models.Handle{Address: "e", DisplayName: "Erik"}
	quoteAnna = &models.Handle{Address: "a", DisplayName: "Anna"}
	quoteLisa = &models.Handle{Address: "l", DisplayName: "Lisa"}
)

// conversation numbers msgs as they are given, a minute apart.
func conversation(msgs ...models.Message) []models.Message {
	out := make([]models.Message, len(msgs))
	for i, msg := range msgs {
		if msg.GUID == "" {
			msg.GUID = string(rune('1' + i))
		}
		msg.DateCreated = 1_700_000_000_000 + int64(i)*60_000
		out[i] = msg
	}
	return out
}

// paneLines renders msgs in a pane width columns wide, without timestamps,
// and returns its lines without colour or trailing padding.
func paneLines(width int, lineNumbers bool, msgs []models.Message) []string {
	m := NewMessagesModel()
	m.SetSize(width, 40)
	m.SetShowTimestamps(false)
	m.SetShowLineNumbers(lineNumbers)
	m.SetMessages(msgs)
	var lines []string
	for _, line := range strings.Split(stripANSI(m.renderedBody), "\n") {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

func TestReplyShowsWhatItAnswers(t *testing.T) {
	forceColor(t)
	lunch := models.Message{GUID: "1", Text: "ska vi ta lunch?", Handle: quoteErik}
	charger := models.Message{GUID: "2", Text: "någon som sett laddaren?", Handle: quoteLisa}
	tests := []struct {
		name        string
		msgs        []models.Message
		noNumbers   bool
		trimIndent  bool     // compare without leading space: outgoing lines are right-aligned
		want        []string // consecutive lines that must be in the pane
		wantNoQuote bool
	}{
		{
			// A quote would repeat the line right above it.
			name:        "answering the message right above",
			msgs:        conversation(lunch, models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"}),
			want:        []string{"#1 Erik: ska vi ta lunch?", "#2 Anna: ↳ ja"},
			wantNoQuote: true,
		},
		{
			// Several replies in a row, as a thread reads when nobody
			// talks in between.
			name: "answering what the reply above answers",
			msgs: conversation(lunch,
				models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"},
				models.Message{Text: "eller hamnen", Handle: quoteErik, ReplyToGUID: "1"}),
			want:        []string{"#2 Anna: ↳ ja", "#3 Erik: ↳ eller hamnen"},
			wantNoQuote: true,
		},
		{
			name: "answering a message further up",
			msgs: conversation(lunch, charger, models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"}),
			want: []string{"#2 Lisa: någon som sett laddaren?", "  ↱ #1 Erik: ska vi ta lunch?", "#3 Anna: ja"},
		},
		{
			name: "answering one that is not loaded",
			msgs: conversation(lunch, models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "gone"}),
			want: []string{"  ↱ reply to an earlier message", "#2 Anna: ja"},
		},
		{
			// A passage the sender picked is what they answer, so it shows
			// even right below the message it came from.
			name: "quoting a passage",
			msgs: conversation(lunch, models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1", ReplyQuote: "lunch"}),
			want: []string{"  ▎ #1 Erik: lunch", "#2 Anna: ja"},
		},
		{
			name: "quoting a passage of one not loaded",
			msgs: conversation(lunch, models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "gone", ReplyQuote: "lunch"}),
			want: []string{"  ▎ lunch", "#2 Anna: ja"},
		},
		{
			name:       "the user's own reply",
			msgs:       conversation(lunch, charger, models.Message{Text: "ja", IsFromMe: true, ReplyToGUID: "1"}),
			trimIndent: true,
			want:       []string{"↱ #1 Erik: ska vi ta lunch?", "#3 You: ja"},
		},
		{
			name: "a reply to the user",
			msgs: conversation(models.Message{Text: "lunch?", IsFromMe: true}, charger,
				models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"}),
			want: []string{"  ↱ #1 You: lunch?", "#3 Anna: ja"},
		},
		{
			name:      "without line numbers",
			msgs:      conversation(lunch, charger, models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"}),
			noNumbers: true,
			want:      []string{"  ↱ Erik: ska vi ta lunch?", "Anna: ja"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := paneLines(60, !tt.noNumbers, tt.msgs)
			if tt.trimIndent {
				for i := range lines {
					lines[i] = strings.TrimSpace(lines[i])
				}
			}
			view := strings.Join(lines, "\n")
			if !strings.Contains(view, strings.Join(tt.want, "\n")) {
				t.Errorf("pane does not show\n%s\n--- pane ---\n%s", strings.Join(tt.want, "\n"), view)
			}
			if tt.wantNoQuote && strings.Contains(view, "↱") {
				t.Errorf("quote drawn where the answered message is right above\n%s", view)
			}
		})
	}
}

// A quote is cut to the pane, in columns: wide glyphs and emoji clusters must
// neither push it past the edge nor be split.
func TestQuoteFitsThePane(t *testing.T) {
	forceColor(t)
	const width = 30
	long := "👨‍👩‍👧‍👦 lunch på 東京ラーメン vid torget 👍🏽 eller hamnen, vad tycker ni om det"
	tests := []struct {
		name     string
		reply    models.Message
		wantRows int
	}{
		{name: "one line", reply: models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"}, wantRows: 1},
		{name: "a passage", reply: models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1", ReplyQuote: long + " " + long}, wantRows: maxQuoteRows},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMessagesModel()
			m.SetSize(width, 40)
			m.SetMessages(conversation(
				models.Message{GUID: "1", Text: long, Handle: quoteErik},
				models.Message{Text: "hm", Handle: quoteLisa},
				tt.reply,
			))
			// One index entry per visual line, or a quote that wrapped
			// instead of being cut would throw every click below it off.
			if lines := strings.Count(m.renderedBody, "\n"); len(m.lineQuotes) != lines {
				t.Fatalf("%d lines but %d index entries\n%s", lines, len(m.lineQuotes), stripANSI(m.renderedBody))
			}
			rows := 0
			for i, line := range strings.Split(m.renderedBody, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("line %d is %d columns in a %d-column pane: %q", i, w, width, stripANSI(line))
				}
				if i < len(m.lineQuotes) && m.lineQuotes[i] != "" {
					rows++
				}
			}
			if rows != tt.wantRows {
				t.Errorf("quote takes %d rows, want %d\n%s", rows, tt.wantRows, stripANSI(m.renderedBody))
			}
		})
	}
}

// A reply appended to a pane on screen draws exactly what a full render of the
// same messages draws, line index included.
func TestAppendedReplyMatchesFullRender(t *testing.T) {
	forceColor(t)
	msgs := conversation(
		models.Message{Text: "ska vi ta lunch?", Handle: quoteErik},
		models.Message{Text: "ja", Handle: quoteAnna, ReplyToGUID: "1"},
		models.Message{Text: "någon som sett laddaren?", Handle: quoteLisa},
		models.Message{Text: "kör", IsFromMe: true, ReplyToGUID: "1"},
		models.Message{Text: "torget", Handle: quoteLisa, ReplyToGUID: "1", ReplyQuote: "lunch"},
		models.Message{Text: "gammalt", Handle: quoteAnna, ReplyToGUID: "gone"},
	)
	inc := NewMessagesModel()
	inc.SetSize(40, 20)
	for _, msg := range msgs {
		inc.AppendMessage(msg)
	}
	full := NewMessagesModel()
	full.SetSize(40, 20)
	full.SetMessages(append([]models.Message(nil), msgs...))

	incBody, incLM, incLL, incSpans := renderState(&inc)
	fullBody, fullLM, fullLL, fullSpans := renderState(&full)
	if incBody != fullBody {
		t.Errorf("body mismatch:\n--- incremental ---\n%s\n--- full ---\n%s", stripANSI(incBody), stripANSI(fullBody))
	}
	if !reflect.DeepEqual(incLM, fullLM) || !reflect.DeepEqual(incLL, fullLL) || !reflect.DeepEqual(incSpans, fullSpans) {
		t.Errorf("line index mismatch:\n incremental=%v\n full=%v", incLM, fullLM)
	}
	if !reflect.DeepEqual(inc.lineQuotes, full.lineQuotes) {
		t.Errorf("lineQuotes mismatch:\n incremental=%q\n full=%q", inc.lineQuotes, full.lineQuotes)
	}
	// Every message block ends in a newline.
	if lines := strings.Count(fullBody, "\n"); len(full.lineQuotes) != lines || len(fullLM) != lines {
		t.Errorf("%d lines, but %d quote entries and %d message entries", lines, len(full.lineQuotes), len(fullLM))
	}
}

// quoteApp is a pane of chatGUID, on a backend with threads or without,
// holding a reply to "root" at the bottom and enough between them that the
// root is out of view.
func quoteApp(t *testing.T, chatGUID string, rootLoaded bool) AppModel {
	t.Helper()
	imessage := &recordingProvider{id: "imessage"}
	slack := &recordingProvider{id: "slack:acme"}
	registry := provider.NewRegistry(imessage)
	registry.Register("sl:acme:", threadingProvider{slack})
	app := NewAppModelWithConfig(registry, nil, nil)
	app.width, app.height = 100, 30
	app.updateLayout()

	chat := models.Chat{GUID: chatGUID, DisplayName: "chat"}
	app.chatList.SetChats([]models.Chat{chat})
	window := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(window, &chat)

	var msgs []models.Message
	if rootLoaded {
		msgs = append(msgs, models.Message{GUID: testRootGUID, Text: "rot", Handle: quoteErik})
	}
	for i := 0; i < 40; i++ {
		msgs = append(msgs, models.Message{GUID: "filler" + string(rune('a'+i)), Text: "prat", Handle: quoteLisa})
	}
	msgs = append(msgs, models.Message{GUID: testReplyGUID, Text: "svar", Handle: quoteAnna, ReplyToGUID: testRootGUID})
	msgs = conversation(msgs...)
	for i := range msgs {
		msgs[i].ChatGUID = chatGUID
	}
	window.Messages.SetMessages(msgs)
	return app
}

// clickQuote clicks the quote row of the focused pane.
func clickQuote(t *testing.T, app AppModel) (AppModel, tea.Cmd) {
	t.Helper()
	window := app.windowManager.FocusedWindow()
	for y := 0; y < window.height; y++ {
		if _, _, ok := window.QuoteAt(y); !ok {
			continue
		}
		model, cmd := app.Update(tea.MouseMsg{
			X:      app.panesX() + window.x + 2,
			Y:      app.tabBarRows() + window.y + y,
			Action: tea.MouseActionPress,
			Button: tea.MouseButtonLeft,
		})
		return model.(AppModel), cmd
	}
	t.Fatalf("no quote row on screen\n%s", stripANSI(window.View()))
	return app, nil
}

// inView reports whether message n's own first line — not a quote of it —
// is inside the viewport.
func inView(m *MessagesModel, n int) bool {
	for line, number := range m.lineMessages {
		if number == n && m.lineQuotes[line] == "" {
			return line >= m.viewport.YOffset && line < m.viewport.YOffset+m.viewport.Height
		}
	}
	return false
}

func TestClickOnAQuoteJumpsToTheAnsweredMessage(t *testing.T) {
	forceColor(t)
	app := quoteApp(t, "chat-a", true)
	window := app.windowManager.FocusedWindow()
	if inView(&window.Messages, 1) {
		t.Fatal("fixture: the answered message is already in view")
	}

	app, _ = clickQuote(t, app)
	window = app.windowManager.FocusedWindow()
	if !inView(&window.Messages, 1) {
		t.Errorf("the answered message is not in view after the click\n%s", stripANSI(window.View()))
	}
	if window.Messages.highlightGUID != testRootGUID {
		t.Errorf("flashing %q, want the answered message", window.Messages.highlightGUID)
	}

	// A refresh while it flashes must not throw the view back to the bottom.
	window.Messages.SetMessages(window.Messages.messages)
	if !inView(&window.Messages, 1) {
		t.Errorf("a refresh scrolled away from the answered message\n%s", stripANSI(window.View()))
	}

	model, _ := app.Update(highlightExpiredMsg{messageGUID: testRootGUID})
	app = model.(AppModel)
	if got := app.windowManager.FocusedWindow().Messages.highlightGUID; got != "" {
		t.Errorf("still flashing %q after it expired", got)
	}
}

func TestClickOnAQuoteOfAMessageNotLoaded(t *testing.T) {
	forceColor(t)
	t.Run("a backend with threads opens the reply's thread", func(t *testing.T) {
		app := quoteApp(t, "sl:acme:C1", false)
		app, _ = clickQuote(t, app)
		if n := app.windowManager.WindowCount(); n != 2 {
			t.Fatalf("got %d panes, want the chat and the thread", n)
		}
		if chat := app.windowManager.FocusedWindow().Chat; chat == nil || chat.GUID != testThreadGUID {
			t.Errorf("focused pane shows %v, want the thread", chat)
		}
	})
	t.Run("any other backend says so", func(t *testing.T) {
		app := quoteApp(t, "chat-a", false)
		app, cmd := clickQuote(t, app)
		if n := app.windowManager.WindowCount(); n != 1 {
			t.Errorf("got %d panes, want the one", n)
		}
		toast := ""
		for _, msg := range cmdMessages(cmd) {
			if tm, ok := msg.(toastMsg); ok {
				toast = tm.text
			}
		}
		if !strings.Contains(toast, "not loaded") {
			t.Errorf("toast = %q, want it to say the message is not loaded", toast)
		}
	})
}
