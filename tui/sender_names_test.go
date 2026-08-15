package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
)

// paneWith loads a conversation into the focused pane of a two-pane app and
// returns the app. The second pane stays empty unless the caller fills it.
func paneWith(t *testing.T, senders ...string) AppModel {
	t.Helper()
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = model.(AppModel)

	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Messages.SetMessages(messagesFrom(senders...))
	app.focused = focusWindow
	return app
}

func messagesFrom(senders ...string) []models.Message {
	messages := make([]models.Message, 0, len(senders))
	for i, sender := range senders {
		messages = append(messages, models.Message{
			GUID:        "m" + sender + string(rune('0'+i)),
			Text:        "rad " + sender,
			DateCreated: int64(1000 + i*1000),
			Handle:      &models.Handle{DisplayName: sender},
		})
	}
	return messages
}

// A conversation with several people keeps its names even when the global
// setting is off — that is the Slack-channel case, and it is what the setting
// exists to answer.
func TestSeveralSendersKeepNamesWhenTheGlobalSettingIsOff(t *testing.T) {
	app := paneWith(t, "Anna", "Bo")
	app.windowManager.SetShowSenderNames(false)

	// Read through the window every time: Messages is a value field, so a
	// local copy would stop tracking the pane.
	if !app.windowManager.FocusedWindow().Messages.ShowingSenderNames() {
		t.Error("a multi-person conversation dropped its names")
	}
	if view := stripANSI(app.windowManager.FocusedWindow().Messages.View()); !strings.Contains(view, "Anna:") {
		t.Errorf("names missing from the view: %q", view)
	}
}

// The two-person case follows the global setting, since there is nothing to
// disambiguate.
func TestTwoPersonChatFollowsTheGlobalSetting(t *testing.T) {
	app := paneWith(t, "Anna", "Anna")
	app.windowManager.SetShowSenderNames(false)

	if app.windowManager.FocusedWindow().Messages.ShowingSenderNames() {
		t.Error("a two-person chat ignored the global setting")
	}

	app.windowManager.SetShowSenderNames(true)
	if !app.windowManager.FocusedWindow().Messages.ShowingSenderNames() {
		t.Error("a two-person chat ignored the global setting being turned back on")
	}
}

// ctrl+b answers for the pane you are looking at, and leaves the others alone.
func TestPerPaneToggleLeavesOtherPanesAlone(t *testing.T) {
	app := paneWith(t, "Anna", "Bo")
	if err := app.windowManager.SplitWindow(SplitHorizontal); err != nil {
		t.Fatalf("split: %v", err)
	}
	other := models.Chat{GUID: "chat-b", DisplayName: "Work"}
	second := app.windowManager.FocusedWindow()
	second.SetChat(&other)
	second.Messages.SetMessages(messagesFrom("Cilla", "Dan"))
	app.updateLayout()

	before := map[WindowID]bool{}
	for _, window := range app.windowManager.AllWindows() {
		before[window.ID] = window.Messages.ShowingSenderNames()
	}

	focused := app.windowManager.FocusedWindow()
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	app = model.(AppModel)

	for _, window := range app.windowManager.AllWindows() {
		got := window.Messages.ShowingSenderNames()
		if window.ID == focused.ID {
			if got == before[window.ID] {
				t.Errorf("the focused pane did not change: still %v", got)
			}
			continue
		}
		if got != before[window.ID] {
			t.Errorf("pane %d changed from %v to %v", window.ID, before[window.ID], got)
		}
	}
}

// A pinned pane ignores the global toggle, or the per-pane answer would be
// undone by the next app-wide change.
func TestPinnedPaneIgnoresTheGlobalToggle(t *testing.T) {
	app := paneWith(t, "Anna", "Bo")
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	app = model.(AppModel)

	pinned := app.windowManager.FocusedWindow().Messages.ShowingSenderNames()

	app.windowManager.SetShowSenderNames(!pinned)
	if got := app.windowManager.FocusedWindow().Messages.ShowingSenderNames(); got != pinned {
		t.Errorf("the global toggle overrode the pane's own answer: %v", got)
	}
}

// The choice belongs to the conversation it was made for.
func TestChangingConversationClearsThePaneChoice(t *testing.T) {
	app := paneWith(t, "Anna", "Bo")
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	app = model.(AppModel)

	window := app.windowManager.FocusedWindow()
	if window.Messages.ShowingSenderNames() {
		t.Fatal("expected the toggle to turn names off for this pane")
	}

	next := models.Chat{GUID: "chat-b", DisplayName: "Work"}
	window.SetChat(&next)
	window.Messages.SetMessages(messagesFrom("Cilla", "Dan"))

	if !window.Messages.ShowingSenderNames() {
		t.Error("the previous conversation's choice carried into the new one")
	}
}

// alt+m still answers for everything, and is what new panes start from.
func TestAllPanesToggleStillWorks(t *testing.T) {
	app := paneWith(t, "Anna", "Anna")
	before := app.showSenderNames

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
	app = model.(AppModel)

	if app.showSenderNames == before {
		t.Fatal("alt+m did not change the app-wide setting")
	}
	if got := app.windowManager.FocusedWindow().Messages.ShowingSenderNames(); got != app.showSenderNames {
		t.Errorf("pane shows %v, app-wide setting is %v", got, app.showSenderNames)
	}
}
