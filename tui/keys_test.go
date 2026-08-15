package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
)

func newComposingApp(t *testing.T) AppModel {
	t.Helper()
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Focused = true
	window.Input.Focus()
	app.focused = focusWindow
	return app
}

func typeRunes(app AppModel, s string) AppModel {
	for _, r := range s {
		model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = model.(AppModel)
	}
	return app
}

// Single-letter shortcuts must not eat characters out of a message. "?" opened
// help, "q" quit the app outright and "G" jumped the viewport, so none of them
// could be typed into a chat.
func TestComposerReceivesShortcutCharacters(t *testing.T) {
	app := typeNewComposer(t, "vad? q G /img")

	if got := app.windowManager.FocusedWindow().Input.GetText(); got != "vad? q G /img" {
		t.Errorf("composer text = %q, want %q", got, "vad? q G /img")
	}
	if app.showHelp {
		t.Error("typing ? opened the help overlay")
	}
}

func typeNewComposer(t *testing.T, s string) AppModel {
	t.Helper()
	return typeRunes(newComposingApp(t), s)
}

// Modified keys are still commands while composing — that is what keeps every
// shortcut reachable now that plain letters are reserved for text.
func TestModifiedKeysStillCommandsWhileComposing(t *testing.T) {
	app := newComposingApp(t)
	before := app.showChatList

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	app = model.(AppModel)

	if app.showChatList == before {
		t.Error("ctrl+s did not toggle the chat list while composing")
	}
	if got := app.windowManager.FocusedWindow().Input.GetText(); got != "" {
		t.Errorf("ctrl+s leaked into the composer as %q", got)
	}
}

// With focus on the chat list and no search running, the letter shortcuts work
// exactly as before.
func TestShortcutsStillWorkOutsideTextFields(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	app.chatList.SetChats([]models.Chat{{GUID: "chat-a", DisplayName: "Family"}})
	app.focused = focusChatList

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app = model.(AppModel)
	if !app.showHelp {
		t.Error("? did not open help from the chat list")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app = model.(AppModel)
	if app.showHelp {
		t.Error("? did not close help again")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = model.(AppModel)
	if app.chatAction != chatActionDelete {
		t.Error("d did not start the delete action from the chat list")
	}
}

// With a chat open the arrow keys move the cursor through the message being
// written. They used to jump between panes, so text could only be appended.
func TestArrowsMoveCursorInActiveChat(t *testing.T) {
	app := newComposingApp(t)
	app.showChatList = true
	app = typeRunes(app, "hejdig")

	for i := 0; i < 3; i++ {
		model, _ := app.Update(tea.KeyMsg{Type: tea.KeyLeft})
		app = model.(AppModel)
	}
	app = typeRunes(app, " ")

	if got := app.windowManager.FocusedWindow().Input.GetText(); got != "hej dig" {
		t.Errorf("composer text = %q, want %q", got, "hej dig")
	}
	if app.focused != focusWindow {
		t.Error("arrow keys moved focus out of the chat")
	}

	// Right brings the cursor back the other way.
	for i := 0; i < 2; i++ {
		model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRight})
		app = model.(AppModel)
	}
	app = typeRunes(app, "X")
	if got := app.windowManager.FocusedWindow().Input.GetText(); got != "hej diXg" {
		t.Errorf("composer text = %q, want %q", got, "hej diXg")
	}
}

// Pane navigation stays reachable mid-message through shift+←/→.
func TestShiftArrowsNavigatePanesWhileComposing(t *testing.T) {
	app := newComposingApp(t)
	app.showChatList = true

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	app = model.(AppModel)
	chat := models.Chat{GUID: "chat-b", DisplayName: "Work"}
	app.windowManager.FocusedWindow().SetChat(&chat)

	right := app.windowManager.FocusedWindow().ID
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	app = model.(AppModel)

	if app.windowManager.FocusedWindow().ID == right {
		t.Error("shift+left did not move to the pane on the left")
	}
	if app.focused != focusWindow {
		t.Error("shift+left left the window focus entirely")
	}
}

// A pane created by a split must be typeable straight away: SetFocus used to
// mark it focused while leaving its composer blurred.
func TestSplitPaneComposerIsTypeable(t *testing.T) {
	app := newComposingApp(t)
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	app = model.(AppModel)

	window := app.windowManager.FocusedWindow()
	if !window.Input.Focused() {
		t.Fatal("composer in the new pane is not focused")
	}

	chat := models.Chat{GUID: "chat-b", DisplayName: "Work"}
	window.SetChat(&chat)
	app = typeRunes(app, "hall")
	if got := app.windowManager.FocusedWindow().Input.GetText(); got != "hall" {
		t.Errorf("split pane composer text = %q, want %q", got, "hall")
	}
}

// Outside a text field the plain arrows still navigate.
func TestArrowsStillNavigateOutsideTextFields(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	app.chatList.SetChats([]models.Chat{{GUID: "chat-a", DisplayName: "Family"}})
	app.focused = focusChatList
	app.showChatList = true

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRight})
	app = model.(AppModel)
	if app.focused != focusWindow {
		t.Error("right from the chat list did not move into the window")
	}
}

// The chat list search box is a text field too: "q" there must filter, not quit.
func TestChatListSearchReceivesShortcutCharacters(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	app.chatList.SetChats([]models.Chat{{GUID: "chat-a", DisplayName: "Q-team"}})
	app.focused = focusChatList

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = model.(AppModel)
	if !app.chatList.SearchActive() {
		t.Fatal("/ did not start search")
	}

	app = typeRunes(app, "q?")
	if got := app.chatList.list.searchQuery; got != "q?" {
		t.Errorf("search query = %q, want %q", got, "q?")
	}
	if app.showHelp {
		t.Error("? opened help while searching")
	}
}

// The help overlay and the empty-pane placeholder both tell the user to press
// F1, so it has to work — for a long time only "?" did.
func TestF1OpensAndClosesHelp(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyF1})
	app = model.(AppModel)
	if !app.showHelp {
		t.Fatal("F1 did not open the help overlay")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyF1})
	app = model.(AppModel)
	if app.showHelp {
		t.Error("F1 did not close the help overlay")
	}
}
