package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
)

// longListApp is an app with the chat list focused and forty chats in it,
// more than fit on screen, named "Chat 00" to "Chat 39".
func longListApp(t *testing.T) AppModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	app := NewAppModelWithConfig(nil, nil, nil)
	app.width, app.height = 100, 20
	app.focused = focusChatList
	app.showChatList = true
	var chats []models.Chat
	for i := 0; i < 40; i++ {
		chats = append(chats, models.Chat{GUID: fmt.Sprintf("chat-%02d", i), DisplayName: fmt.Sprintf("Chat %02d", i)})
	}
	app.chatList.SetChats(chats)
	app.updateLayout()
	return app
}

// selectedIndex is the number of the chat the list cursor is on.
func selectedIndex(t *testing.T, app AppModel) int {
	t.Helper()
	chat := app.chatList.SelectedChat()
	if chat == nil {
		t.Fatal("no chat selected")
	}
	var i int
	if _, err := fmt.Sscanf(chat.GUID, "chat-%d", &i); err != nil {
		t.Fatalf("selected %s: %v", chat.GUID, err)
	}
	return i
}

func listShows(app AppModel, name string) bool {
	return strings.Contains(stripANSI(app.chatList.View()), name)
}

// PgUp/PgDn, and shift+↑/↓ for keyboards without them, move the cursor a
// screenful of chats at a time and stop at either end.
func TestPageKeysMoveThroughTheChatList(t *testing.T) {
	tests := []struct {
		name     string
		down, up tea.KeyMsg
	}{
		{"page keys", tea.KeyMsg{Type: tea.KeyPgDown}, tea.KeyMsg{Type: tea.KeyPgUp}},
		{"shift+arrows", tea.KeyMsg{Type: tea.KeyShiftDown}, tea.KeyMsg{Type: tea.KeyShiftUp}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := longListApp(t)
			page := app.chatList.list.pageSize()
			if page < 2 || page >= 40 {
				t.Fatalf("%d chats on screen, want a list longer than the screen", page)
			}

			app = press(t, app, tt.down)
			if got := selectedIndex(t, app); got != page {
				t.Fatalf("selected chat %d after a page down, want %d", got, page)
			}
			if !listShows(app, fmt.Sprintf("Chat %02d", page)) {
				t.Errorf("the selected chat is not on screen")
			}
			for i := 0; i < 10; i++ {
				app = press(t, app, tt.down)
			}
			if got := selectedIndex(t, app); got != 39 {
				t.Errorf("selected chat %d, want the last one", got)
			}
			app = press(t, app, tt.up)
			if got := selectedIndex(t, app); got != 39-page {
				t.Errorf("selected chat %d after a page up, want %d", got, 39-page)
			}
			for i := 0; i < 10; i++ {
				app = press(t, app, tt.up)
			}
			if got := selectedIndex(t, app); got != 0 {
				t.Errorf("selected chat %d, want the first one", got)
			}
		})
	}
}

// In a pane, shift+↑/↓ page through the history like PgUp/PgDn.
func TestShiftArrowsPageThroughAPane(t *testing.T) {
	app := quoteApp(t, "im-1", true)
	app.focused = focusWindow
	window := app.windowManager.FocusedWindow()
	bottom := window.Messages.viewport.YOffset

	app = press(t, app, tea.KeyMsg{Type: tea.KeyShiftUp})
	window = app.windowManager.FocusedWindow()
	if got := window.Messages.viewport.YOffset; got >= bottom {
		t.Fatalf("offset %d after shift+up, want above %d", got, bottom)
	}
	app = press(t, app, tea.KeyMsg{Type: tea.KeyShiftDown})
	if got := app.windowManager.FocusedWindow().Messages.viewport.YOffset; got != bottom {
		t.Errorf("offset %d after shift+down, want back at %d", got, bottom)
	}
}

func wheel(t *testing.T, app AppModel, x int, button tea.MouseButton) AppModel {
	t.Helper()
	model, _ := app.Update(tea.MouseMsg{X: x, Y: 5, Action: tea.MouseActionPress, Button: button})
	return model.(AppModel)
}

// The wheel scrolls the list without choosing a chat: the cursor stays put
// while its row is on screen and is carried along at the edge, so it never
// leaves the screen — which would snap the list back on the next message.
func TestWheelScrollsTheChatList(t *testing.T) {
	app := longListApp(t)
	app.focused = focusWindow
	app.chatList.SelectChatByGUID("chat-05")

	app = wheel(t, app, 2, tea.MouseButtonWheelDown)
	if got := app.chatList.list.offset; got != listWheelRows {
		t.Fatalf("offset %d after one step, want %d", got, listWheelRows)
	}
	if got := selectedIndex(t, app); got != 5 {
		t.Errorf("selected chat %d, want the cursor left on chat 5 while it is in view", got)
	}
	if app.focused != focusWindow {
		t.Error("the wheel moved the focus")
	}

	for i := 0; i < 20; i++ {
		app = wheel(t, app, 2, tea.MouseButtonWheelDown)
	}
	if !listShows(app, "Chat 39") {
		t.Error("scrolled to the end without showing the last chat")
	}
	if listShows(app, "Chat 05") {
		t.Fatal("chat 5 is still on screen at the end of the list")
	}
	if got := selectedIndex(t, app); !listShows(app, fmt.Sprintf("Chat %02d", got)) {
		t.Errorf("selected chat %d is off screen", got)
	}
	end := app.chatList.list.offset
	app = wheel(t, app, 2, tea.MouseButtonWheelDown)
	if got := app.chatList.list.offset; got != end {
		t.Errorf("offset %d, want it to stop at %d", got, end)
	}

	for i := 0; i < 20; i++ {
		app = wheel(t, app, 2, tea.MouseButtonWheelUp)
	}
	if got := app.chatList.list.offset; got != 0 {
		t.Errorf("offset %d after scrolling back up, want 0", got)
	}
}

// searchApp is an app showing the chat list, with a pane on Anna.
func searchApp(t *testing.T) AppModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	app := NewAppModelWithConfig(nil, nil, nil)
	app.width, app.height = 100, 30
	app.showChatList = true
	var chats []models.Chat
	for _, name := range []string{"Anna", "Bob", "Cecilia", "Engineering", "Erik", "Family"} {
		chats = append(chats, models.Chat{GUID: strings.ToLower(name), DisplayName: name})
	}
	app.chatList.SetChats(chats)
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &chats[0])
	app.focused = focusChatList
	app.updateLayout()
	return app
}

func paneChat(app AppModel) string {
	if w := app.windowManager.FocusedWindow(); w != nil && w.Chat != nil {
		return w.Chat.GUID
	}
	return ""
}

// ↑/↓ pick among the matches, and Enter opens the one picked.
func TestSearchPicksAMatchAndEnterOpensIt(t *testing.T) {
	app := searchApp(t)
	app = typeRunes(app, "/e")
	if got := app.chatList.SelectedChat().GUID; got != "cecilia" {
		t.Fatalf("selected %s, want the first match", got)
	}
	app = press(t, app, tea.KeyMsg{Type: tea.KeyDown})
	app = press(t, app, tea.KeyMsg{Type: tea.KeyDown})
	if got := app.chatList.SelectedChat().GUID; got != "erik" {
		t.Fatalf("selected %s, want ↓ to step to the third match", got)
	}

	app = press(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.chatList.SearchActive() {
		t.Error("search still open after Enter")
	}
	if got := paneChat(app); got != "erik" {
		t.Errorf("pane shows %s, want the chat picked", got)
	}
	if got := app.chatList.SelectedChat().GUID; got != "erik" {
		t.Errorf("list cursor on %s, want it on the chat opened", got)
	}
	if app.focused != focusWindow {
		t.Error("focus stayed in the list")
	}
}

// Esc ends the search on the chat it found, or, with nothing found, back on
// the chat selected before it.
func TestEscEndsSearchOnTheChatFound(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{"erik", "erik"},
		{"zzz", "bob"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			app := searchApp(t)
			app.chatList.SelectChatByGUID("bob")
			app = typeRunes(app, "/"+tt.query)
			app = press(t, app, tea.KeyMsg{Type: tea.KeyEsc})

			if app.chatList.SearchActive() {
				t.Fatal("search still open after Esc")
			}
			if got := app.chatList.SelectedChat().GUID; got != tt.want {
				t.Errorf("selected %s, want %s", got, tt.want)
			}
			if got := paneChat(app); got != "anna" {
				t.Errorf("pane shows %s, want Esc to open nothing", got)
			}
		})
	}
}

// ctrl+k finds a chat from a pane, mid-message, with the list hidden: the
// list shows for the search and goes again once it ends, either way.
func TestCtrlKFindsAChatFromAnywhere(t *testing.T) {
	ctrlK := tea.KeyMsg{Type: tea.KeyCtrlK}
	for _, end := range []string{"enter", "esc"} {
		t.Run(end, func(t *testing.T) {
			app := searchApp(t)
			app.showChatList = false
			app.focused = focusWindow
			app.updateLayout()
			app = typeRunes(app, "hej")

			app = press(t, app, ctrlK)
			if !app.showChatList || app.focused != focusChatList || !app.chatList.SearchActive() {
				t.Fatalf("after ctrl+k: list shown %v, focus %v, searching %v", app.showChatList, app.focused, app.chatList.SearchActive())
			}
			app = typeRunes(app, "fam")
			app.saveUIState()
			app.persist.flushAll()
			if config.LoadUIState().ShowChatList {
				t.Fatal("a save during the search recorded the list as shown")
			}

			want := "anna"
			if end == "enter" {
				app = press(t, app, tea.KeyMsg{Type: tea.KeyEnter})
				want = "family"
			} else {
				app = press(t, app, tea.KeyMsg{Type: tea.KeyEsc})
			}
			if app.showChatList || app.focused != focusWindow || app.chatList.SearchActive() {
				t.Errorf("after %s: list shown %v, focus %v, searching %v", end, app.showChatList, app.focused, app.chatList.SearchActive())
			}
			if got := paneChat(app); got != want {
				t.Errorf("pane shows %s, want %s", got, want)
			}
		})
	}

	// With the list on screen already, it stays there.
	app := searchApp(t)
	app.focused = focusWindow
	app = press(t, app, ctrlK)
	app = typeRunes(app, "bob")
	app = press(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if !app.showChatList {
		t.Error("ctrl+k hid a list that was on screen before it")
	}
	if got := paneChat(app); got != "bob" {
		t.Errorf("pane shows %s, want bob", got)
	}
}
