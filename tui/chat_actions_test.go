package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
)

func TestAppModelDeleteChatRequiresConfirm(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	app.chatList.SetChats([]models.Chat{{GUID: "chat-a", DisplayName: "Family"}})
	app.focused = focusChatList

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = model.(AppModel)
	if !strings.Contains(app.statusBarView(), "Press D to confirm") {
		t.Fatalf("delete confirmation not shown: %q", app.statusBarView())
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = model.(AppModel)
	if strings.Contains(app.statusBarView(), "Press D to confirm") {
		t.Fatalf("delete confirmation did not cancel: %q", app.statusBarView())
	}
}

func TestAppModelRenamePromptCapturesText(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	app.chatList.SetChats([]models.Chat{{GUID: "chat-a", DisplayName: "Family"}})
	app.focused = focusChatList

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	app = model.(AppModel)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	app = model.(AppModel)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	app = model.(AppModel)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	app = model.(AppModel)

	if !strings.Contains(app.statusBarView(), "Rename") || !strings.Contains(app.statusBarView(), "New") {
		t.Fatalf("rename prompt did not capture text: %q", app.statusBarView())
	}
}

func TestAppModelWindowChatActionsUseControlKeys(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.windowManager.FocusedWindow().SetChat(&chat)
	app.focused = focusWindow

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	app = model.(AppModel)
	if !strings.Contains(app.statusBarView(), "Press D to confirm") {
		t.Fatalf("delete confirmation not shown from pane: %q", app.statusBarView())
	}
}

func TestAppModelDeleteChatSuccessRemovesLocalState(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetSize(40, 8)
	app.chatList.SetChats([]models.Chat{chat})
	app.windowManager.FocusedWindow().SetChat(&chat)
	app.windowManager.SetCachedMessages("chat-a", []models.Message{{GUID: "message-a", Text: "hello"}})

	model, _ := app.Update(deleteChatSuccessMsg{chatGUID: "chat-a"})
	app = model.(AppModel)

	if strings.Contains(app.chatList.View(), "Family") {
		t.Fatalf("deleted chat still visible: %q", app.chatList.View())
	}
	if got := app.windowManager.FocusedWindow(); got == nil || got.Chat != nil {
		t.Fatalf("deleted chat still open in pane")
	}
	if cached := app.windowManager.GetCachedMessages("chat-a"); len(cached) != 0 {
		t.Fatalf("deleted chat messages still cached: %#v", cached)
	}
}

func TestAppModelRenameErrorAppliesLocalAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetSize(40, 8)
	app.chatList.SetChats([]models.Chat{chat})
	app.windowManager.FocusedWindow().SetChat(&chat)

	model, _ := app.Update(renameChatErrMsg{
		chatGUID:    "chat-a",
		displayName: "Renamed Family",
		err:         fmt.Errorf("private api unsupported"),
	})
	app = model.(AppModel)

	if !strings.Contains(app.chatList.View(), "Renamed Family") {
		t.Fatalf("renamed chat not visible in list: %q", app.chatList.View())
	}
	if got := app.windowManager.FocusedWindow().Chat.GetDisplayName(); got != "Renamed Family" {
		t.Fatalf("pane chat name = %q, want Renamed Family", got)
	}
	if got := app.chatOverrides.Aliases["chat-a"]; got != "Renamed Family" {
		t.Fatalf("alias = %q, want Renamed Family", got)
	}
}
