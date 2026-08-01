package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
)

func newRenderedApp(t *testing.T, width, height int) AppModel {
	t.Helper()
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Messages.SetMessages([]models.Message{
		{GUID: "m1", Text: "incoming", DateCreated: time.Now().UnixMilli(),
			Handle: &models.Handle{DisplayName: "Anna"}},
		{GUID: "m2", Text: "outgoing", DateCreated: time.Now().UnixMilli(), IsFromMe: true},
	})
	model, _ := app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return model.(AppModel)
}

// The frame must occupy exactly the terminal size. One row too many and the
// alt screen scrolls, which used to clip the top row off.
func TestViewFillsTerminalExactly(t *testing.T) {
	for _, height := range []int{10, 24, 40} {
		view := newRenderedApp(t, 80, height).View()
		if got := lipgloss.Height(view); got != height {
			t.Errorf("height %d: rendered %d rows, want %d", height, got, height)
		}
		if got := lipgloss.Width(view); got > 80 {
			t.Errorf("height %d: rendered width %d, want <= 80", height, got)
		}
	}
}

// There is no persistent status bar. Prompts the user has to see still get
// surfaced, but they overlay the bottom row instead of claiming one.
func TestStatusLineOverlaysBottomRowOnly(t *testing.T) {
	app := newRenderedApp(t, 80, 24)
	if line := app.statusLineView(); line != "" {
		t.Errorf("idle status line should be empty, got %q", line)
	}

	app.focused = focusChatList
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = model.(AppModel)

	view := app.View()
	if got := lipgloss.Height(view); got != 24 {
		t.Errorf("with status line: %d rows, want 24", got)
	}
	rows := strings.Split(view, "\n")
	if !strings.Contains(rows[len(rows)-1], "Press D to confirm") {
		t.Errorf("status line not on bottom row: %q", rows[len(rows)-1])
	}
	if strings.Contains(rows[0], "Press D to confirm") {
		t.Errorf("status line leaked to top row: %q", rows[0])
	}
}

// Nothing rendered may depend on terminal background detection being right.
// ANSI 7/15 (white) and 0 (black) foregrounds, and the 252/235/86/27 pairs the
// old adaptive palette used, all go invisible on one background or the other.
func TestNoBackgroundDependentColorsInView(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	app := newRenderedApp(t, 80, 12)
	app.focused = focusWindow
	window := app.windowManager.FocusedWindow()
	window.Focused = true
	window.Input.Focus()
	for _, r := range "typed text" {
		model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = model.(AppModel)
	}

	view := app.View()
	if !strings.Contains(view, "typed text") {
		t.Fatal("composer text missing from view")
	}

	banned := map[string]string{
		"\x1b[37m":  "ANSI white foreground",
		"\x1b[30m":  "ANSI black foreground",
		"\x1b[47m":  "ANSI white background",
		"\x1b[40m":  "ANSI black background",
		"38;5;86m":  "old dark-only cyan",
		"38;5;252m": "old dark-only light gray",
		"38;5;27m":  "old light-only blue",
		"38;5;235m": "old light-only dark gray",
		"48;5;235m": "old dark-only overlay background",
	}
	for code, what := range banned {
		if strings.Contains(view, code) {
			t.Errorf("%s still rendered (%q)", what, strings.ReplaceAll(code, "\x1b", "\\e"))
		}
	}
}

// The help overlay is a full-screen replacement, so it must not paint its own
// background either.
func TestHelpOverlayHasNoHardcodedBackground(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	view := helpOverlayView(100, 40)
	for _, code := range []string{"48;5;235m", "\x1b[40m", "\x1b[47m"} {
		if strings.Contains(view, code) {
			t.Errorf("help overlay paints a background (%q)", strings.ReplaceAll(code, "\x1b", "\\e"))
		}
	}
}
