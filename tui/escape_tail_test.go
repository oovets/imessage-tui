package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
)

// typingPane returns an app with a focused composer holding text.
func typingPane(t *testing.T, draft string) AppModel {
	t.Helper()
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Input.textarea.Focus()
	window.Input.textarea.SetValue(draft)
	app.focused = focusWindow
	// The chat list is hidden so Escape has nowhere to move focus to, keeping
	// these tests about the leftover text and nothing else.
	app.showChatList = false
	return app
}

func draftOf(app AppModel) string {
	return app.windowManager.FocusedWindow().Input.GetText()
}

func send(app AppModel, msgs ...tea.KeyMsg) AppModel {
	for _, msg := range msgs {
		model, _ := app.Update(msg)
		switch typed := model.(type) {
		case AppModel:
			app = typed
		case *AppModel:
			app = *typed
		}
	}
	return app
}

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// The reported bug: ctrl+shift+→ arrives as an introducer plus plain runes, and
// "[1;6C" is typed into the pane being switched away from.
func TestTornEscapeSequenceDoesNotBecomeText(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyMsg
	}{
		{
			name: "introducer arrives as alt+[",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true},
				runes("1;6C"),
			},
		},
		{
			name: "esc arrives on its own",
			keys: []tea.KeyMsg{
				{Type: tea.KeyEscape},
				runes("[1;6C"),
			},
		},
		{
			name: "sequence torn into three pieces",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true},
				runes("1;"),
				runes("6C"),
			},
		},
		{
			name: "modifyOtherKeys sequence",
			keys: []tea.KeyMsg{
				{Type: tea.KeyEscape},
				runes("[27;5;9~"),
			},
		},
		{
			// The one the user actually hit: an SGR mouse report torn in half.
			name: "sgr mouse report without its introducer",
			keys: []tea.KeyMsg{runes("<35;15;33M")},
		},
		{
			name: "sgr mouse report with introducer",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true},
				runes("<0;4;7m"),
			},
		},
		{
			name: "ss3 sequence",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune{'O'}, Alt: true},
				runes("c"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := send(typingPane(t, "hej hej"), tt.keys...)
			if got := draftOf(app); got != "hej hej" {
				t.Errorf("draft = %q, want it untouched", got)
			}
		})
	}
}

// The guard must not eat real typing. Without a torn start immediately before,
// text that happens to look like a sequence is just text.
func TestSequenceLookalikeTextIsStillTyped(t *testing.T) {
	app := send(typingPane(t, ""), runes("[1;6C"))
	if got := draftOf(app); got != "[1;6C" {
		t.Errorf("draft = %q, want the text to survive", got)
	}
}

func TestOrdinaryTypingAfterEscapeIsKept(t *testing.T) {
	app := send(typingPane(t, ""), tea.KeyMsg{Type: tea.KeyEscape}, runes("hej"))
	if got := draftOf(app); got != "hej" {
		t.Errorf("draft = %q, want hej", got)
	}
}

// The window closes on its own, so an Escape pressed a while ago cannot swallow
// what is typed later.
func TestTailWindowExpires(t *testing.T) {
	app := typingPane(t, "")
	app = send(app, tea.KeyMsg{Type: tea.KeyEscape})
	app.lastEscapeAt = time.Now().Add(-2 * escapeTailWindow)
	app = send(app, runes("1;6C"))
	if got := draftOf(app); got != "1;6C" {
		t.Errorf("draft = %q, want the stale window to have expired", got)
	}
}

// A real Escape still has to reach the handlers — it is how you get back to the
// chat list — so only the leftovers are dropped, never the key itself.
func TestEscapeItselfStillWorks(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Input.textarea.Focus()
	app.focused = focusWindow
	app.showChatList = true

	app = send(app, tea.KeyMsg{Type: tea.KeyEscape})
	if app.focused != focusChatList {
		t.Error("Escape no longer returns to the chat list")
	}
}

// A mouse report needs no recent Escape to be recognised: by the time one is
// torn the app has usually been busy long enough for any timing window to have
// closed, which is why the first version of this guard missed them.
func TestMouseReportTailIsDroppedWithoutATornStart(t *testing.T) {
	app := send(typingPane(t, "hej"), runes("<35;15;33M"))
	if got := draftOf(app); got != "hej" {
		t.Errorf("draft = %q, want the mouse report dropped", got)
	}
}

// Text that merely contains angle brackets and digits is still text.
func TestMouseReportGuardDoesNotEatOrdinaryText(t *testing.T) {
	// "<3" is deliberately absent: the composer turns it into a heart, which
	// is a documented feature and not this guard's doing.
	for _, text := range []string{"35;15;33M", "<a;b;c>", "<35;15;33", "<>"} {
		app := send(typingPane(t, ""), runes(text))
		if got := draftOf(app); got != text {
			t.Errorf("typing %q produced %q", text, got)
		}
	}
}
