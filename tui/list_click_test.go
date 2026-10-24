package tui

import (
	"testing"
	"time"

	"github.com/oovets/imessage-tui/models"
)

// A double click on a chat in the list opens it in the focused pane, the way
// Enter does. A single click only selects, and two clicks that are on
// different rows, too far apart, or on a header are not a double click.
func TestDoubleClickOpensAChatInTheFocusedPane(t *testing.T) {
	tests := []struct {
		name   string
		clicks []string // the row text clicked, in order
		slow   bool     // the second click comes a second later
		want   string   // the chat the focused pane shows after, "" for none
		typing bool     // focus ends in the pane's composer, not the list
	}{
		{name: "double click", clicks: []string{"Family", "Family"}, want: "chat-a", typing: true},
		{name: "single click", clicks: []string{"Family"}},
		{name: "two chats", clicks: []string{"Family", "Bob"}},
		{name: "too slow", clicks: []string{"Family", "Family"}, slow: true},
		{name: "on a header", clicks: []string{"Pinned", "Pinned"}},
		// The third click is a click of its own, which puts focus on the list.
		{name: "triple click opens once", clicks: []string{"Bob", "Bob", "Bob"}, want: "chat-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTabTestApp(t, 100, 30)
			app.showChatList = true
			app.chatList.SetChats([]models.Chat{
				{GUID: "chat-a", DisplayName: "Family"},
				{GUID: "chat-b", DisplayName: "Bob"},
			})
			app.chatList.SetPinned([]string{"chat-a"})
			app.focused = focusWindow
			app.updateLayout()
			pane := app.windowManager.FocusedWindow()

			rows := make(map[string]int)
			for _, text := range tt.clicks {
				if rows[text] = rowContaining(app.View(), text); rows[text] < 0 {
					t.Fatalf("%q not on screen", text)
				}
			}
			for i, text := range tt.clicks {
				if i > 0 && tt.slow {
					app.listClickAt = time.Now().Add(-time.Second)
				}
				app = click(t, app, 3, rows[text])
			}

			got := ""
			if pane.Chat != nil {
				got = pane.Chat.GUID
			}
			if got != tt.want {
				t.Errorf("pane shows %q, want %q", got, tt.want)
			}
			wantFocus := focusChatList
			if tt.typing {
				wantFocus = focusWindow
				if !pane.Input.Focused() {
					t.Error("the composer does not have focus")
				}
			}
			if app.focused != wantFocus {
				t.Errorf("focus %v, want %v", app.focused, wantFocus)
			}
		})
	}
}
