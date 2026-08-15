package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
	"github.com/oovets/imessage-tui/provider/imessage"
)

func TestParseReplyCommand(t *testing.T) {
	tests := []struct {
		raw     string
		target  int
		text    string
		handled bool
		wantErr bool
	}{
		{raw: "/r #3 hej där", target: 3, text: "hej där", handled: true},
		{raw: "/r 3 hej", target: 3, text: "hej", handled: true},
		{raw: "/r #12 flera ord  här", target: 12, text: "flera ord  här", handled: true},
		{raw: "/r #3", handled: true, wantErr: true},
		{raw: "/r", handled: true, wantErr: true},
		{raw: "/r #x hej", handled: true, wantErr: true},
		{raw: "/r #0 hej", handled: true, wantErr: true},
		// Not the command: ordinary text that happens to start with r, and
		// other slash commands.
		{raw: "/rocket ship", handled: false},
		{raw: "regular message", handled: false},
		{raw: "/img #2", handled: false},
	}

	for _, tt := range tests {
		target, text, handled, err := parseReplyCommand(tt.raw)
		if handled != tt.handled {
			t.Errorf("%q: handled = %v, want %v", tt.raw, handled, tt.handled)
			continue
		}
		if !tt.handled {
			continue
		}
		if tt.wantErr {
			if err == nil {
				t.Errorf("%q: want an error", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.raw, err)
			continue
		}
		if target != tt.target || text != tt.text {
			t.Errorf("%q: got #%d %q, want #%d %q", tt.raw, target, text, tt.target, tt.text)
		}
	}
}

// The reply has to carry the target message's GUID all the way to the backend:
// that is what makes it land in a Slack thread instead of beside it.
func TestReplyCommandSendsSelectedMessageGUID(t *testing.T) {
	var payload map[string]any
	sent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		close(sent)
	}))
	defer server.Close()

	app := NewAppModelWithConfig(provider.NewRegistry(imessage.New(api.NewClient(server.URL, "secret"))), nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	app.chatList.SetChats([]models.Chat{chat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Messages.SetMessages([]models.Message{
		{GUID: "message-a", Text: "första", DateCreated: 1000},
		{GUID: "message-b", Text: "andra", DateCreated: 2000},
	})
	window.Input.textarea.SetValue("/r #1 svar på första")
	app.focused = focusWindow

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a send command")
	}
	// The batch also carries the pending-echo timeout, which sleeps for half a
	// minute — wait for the request itself rather than for every command.
	go runTestCmd(cmd)
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("no message was sent")
	}

	if got := payload["selectedMessageGuid"]; got != "message-a" {
		t.Errorf("selectedMessageGuid = %v, want message-a", got)
	}
	if got := payload["message"]; got != "svar på första" {
		t.Errorf("message = %v", got)
	}
}

func TestReplyCommandRejectsAMissingRow(t *testing.T) {
	app := NewAppModelWithConfig(nil, nil, nil)
	chat := models.Chat{GUID: "chat-a", DisplayName: "Family"}
	window := app.windowManager.FocusedWindow()
	window.SetChat(&chat)
	window.Messages.SetMessages([]models.Message{{GUID: "message-a", Text: "bara en", DateCreated: 1000}})

	cmd, handled := app.handleLocalInputCommand(window, "/r #9 svar")
	if !handled {
		t.Fatal("command not handled")
	}
	if cmd != nil {
		t.Error("a reply to a row that is not on screen was still sent")
	}
	if app.err == nil {
		t.Error("no error surfaced to the user")
	}
}
