package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
)

// wordBackward is what bubbles binds to WordBackward: alt+b and alt+left.
var wordBackwardKeys = []tea.KeyMsg{
	{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true},
	{Type: tea.KeyLeft, Alt: true},
}

// A word-backward key with nothing to move to used to spin forever inside
// bubbles, pegging a core and locking the app with no way back. Every case
// below hung before the guard.
func TestWordBackwardNeverHangs(t *testing.T) {
	drafts := map[string]int{
		"":            0, // empty composer — what the user hit
		"   ":         3, // only spaces before the cursor
		"hej":         0, // cursor at the start of a line
		"  hej":       2, // spaces before, word after
		"hej hopp":    4, // a real word to move to
		"hej\n":       4, // second line, empty
		"hej\n  hopp": 6, // second line, cursor inside the leading spaces
	}

	for draft, column := range drafts {
		draft, column := draft, column
		t.Run(draft, func(t *testing.T) {
			for _, key := range wordBackwardKeys {
				input := NewInputModel()
				input.SetSize(40, 3)
				input.textarea.Focus()
				input.textarea.SetValue(draft)
				input.textarea.SetCursor(column)

				done := make(chan struct{})
				go func() {
					input.Update(key)
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatalf("%q at column %d: %v hung", draft, column, key)
				}
			}
		})
	}
}

// The key still has to work where there is a word to move to.
func TestWordBackwardStillMovesWhenThereIsAWord(t *testing.T) {
	input := NewInputModel()
	input.SetSize(40, 3)
	input.textarea.Focus()
	input.textarea.SetValue("hej hopp")
	input.textarea.SetCursor(8)

	before := input.textarea.LineInfo().ColumnOffset
	updated, _ := input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true})
	after := updated.textarea.LineInfo().ColumnOffset

	if after >= before {
		t.Errorf("cursor stayed at column %d, want it moved back from %d", after, before)
	}
}

// The whole app must survive the key, not just the composer in isolation.
func TestAppSurvivesWordBackwardOnAnEmptyComposer(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = model.(AppModel)

	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Input.textarea.Focus()
	app.focused = focusWindow

	done := make(chan struct{})
	go func() {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("alt+b locked the app")
	}
}
