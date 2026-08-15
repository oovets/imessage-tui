package tui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// recordingProvider stands in for a backend and remembers what it was asked to
// do, so a test can assert which one a chat reached.
type recordingProvider struct {
	id string

	mu    sync.Mutex
	sent  []string
	asked []string
	chats []models.Chat
}

func (r *recordingProvider) ID() string { return r.id }

func (r *recordingProvider) Chats(int) ([]models.Chat, error) {
	return r.chats, nil
}

func (r *recordingProvider) Messages(chatGUID string, _ int) ([]models.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, chatGUID)
	return nil, nil
}

func (r *recordingProvider) Send(chatGUID, text, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, chatGUID+"|"+text)
	return nil
}

func (r *recordingProvider) React(string, string, string) error { return nil }
func (r *recordingProvider) MarkRead(string) error              { return nil }

func (r *recordingProvider) sentMessages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

func twoBackends(t *testing.T) (*recordingProvider, *recordingProvider, AppModel) {
	t.Helper()
	imessage := &recordingProvider{
		id:    "imessage",
		chats: []models.Chat{{GUID: "chat-a", DisplayName: "Family"}},
	}
	slack := &recordingProvider{
		id:    "slack:acme",
		chats: []models.Chat{{GUID: "sl:acme:C1", DisplayName: "#general"}},
	}
	registry := provider.NewRegistry(imessage)
	registry.Register("sl:acme:", slack)
	return imessage, slack, NewAppModelWithConfig(registry, nil, nil)
}

// Sending must reach the backend that owns the chat. Getting this wrong posts
// a private message to the wrong service entirely.
func TestSendRoutesToTheBackendOwningTheChat(t *testing.T) {
	imessage, slack, app := twoBackends(t)

	slackChat := models.Chat{GUID: "sl:acme:C1", DisplayName: "#general"}
	app.chatList.SetChats([]models.Chat{slackChat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&slackChat)
	window.Input.textarea.SetValue("hej Slack")
	app.focused = focusWindow

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a send command")
	}
	go runTestCmd(cmd)

	waitFor(t, func() bool { return len(slack.sentMessages()) > 0 })
	if got := slack.sentMessages()[0]; got != "sl:acme:C1|hej Slack" {
		t.Errorf("slack received %q", got)
	}
	if got := imessage.sentMessages(); len(got) != 0 {
		t.Errorf("iMessage also received %v", got)
	}
}

// The chat list is one list over several backends, and loading it must not be
// all-or-nothing per backend.
func TestChatListMergesEveryBackend(t *testing.T) {
	_, _, app := twoBackends(t)

	cmd := app.Init()
	if cmd == nil {
		t.Fatal("Init produced no commands")
	}

	msg := drainForChats(cmd)
	if msg == nil {
		t.Fatal("no chats loaded")
	}
	chats := []models.Chat(*msg)
	if len(chats) != 2 {
		t.Fatalf("got %d chats, want one from each backend: %+v", len(chats), chats)
	}
	// iMessage first: it is the registry's fallback, and a stable order beats
	// whichever backend answered first.
	if chats[0].GUID != "chat-a" || chats[1].GUID != "sl:acme:C1" {
		t.Errorf("order = %q, %q", chats[0].GUID, chats[1].GUID)
	}
}

// A realtime event from Slack has to flow through the same path an iMessage
// event does: cached, appended to open panes, and marked unread.
func TestSlackEventReachesTheOpenPane(t *testing.T) {
	_, _, app := twoBackends(t)

	slackChat := models.Chat{GUID: "sl:acme:C1", DisplayName: "#general"}
	app.chatList.SetChats([]models.Chat{slackChat})
	window := app.windowManager.FocusedWindow()
	window.SetChat(&slackChat)

	model, _ := app.Update(providerEventMsg(provider.Event{
		Kind:     provider.EventNewMessage,
		ChatGUID: "sl:acme:C1",
		Message: models.Message{
			GUID:        "sl:acme:C1:1700000100.000100",
			Text:        "hej från Slack",
			DateCreated: 1700000100000,
			ChatGUID:    "sl:acme:C1",
			Handle:      &models.Handle{DisplayName: "Anna"},
		},
	}))
	app = *model.(*AppModel)

	if got := stripANSI(window.Messages.View()); !strings.Contains(got, "hej från Slack") {
		t.Errorf("message not rendered in the pane: %q", got)
	}
	if cached := app.windowManager.GetCachedMessages("sl:acme:C1"); len(cached) != 1 {
		t.Errorf("cached %d messages, want 1", len(cached))
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the backend to be called")
}

// drainForChats runs a command tree and returns the chats-loaded message it
// produces, ignoring the timers batched alongside it.
func drainForChats(cmd tea.Cmd) *chatsLoadedMsg {
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if loaded, ok := msg.(chatsLoadedMsg); ok {
			return &loaded
		}
		return nil
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		if loaded, ok := child().(chatsLoadedMsg); ok {
			return &loaded
		}
	}
	return nil
}

// BlueBubbles without the private API answers 404 to every mark-read. That
// must not silence Slack's read receipts, which do work.
func TestReadSyncIsDisabledPerBackend(t *testing.T) {
	_, _, app := twoBackends(t)

	model, _ := app.Update(markReadErrMsg{
		chatGUID: "chat-a",
		err:      errors.New("API error: private api unavailable (status 404)"),
	})
	app = model.(AppModel)

	if cmd := app.markChatReadIfNeeded("chat-a"); cmd != nil {
		t.Error("iMessage read sync stayed on after a 404")
	}
	if cmd := app.markChatReadIfNeeded("sl:acme:C1"); cmd == nil {
		t.Error("Slack read sync was disabled by an iMessage failure")
	}
}

// Slack answers a mark-read it has no scope for with missing_scope. Retrying
// that on every chat the user opens only fills the log.
func TestMissingScopeStopsSlackReadSync(t *testing.T) {
	_, _, app := twoBackends(t)

	model, _ := app.Update(markReadErrMsg{
		chatGUID: "sl:acme:C1",
		err:      errors.New("slack mark read: missing_scope"),
	})
	app = model.(AppModel)

	if cmd := app.markChatReadIfNeeded("sl:acme:C1"); cmd != nil {
		t.Error("Slack read sync kept retrying after missing_scope")
	}
	if cmd := app.markChatReadIfNeeded("chat-a"); cmd == nil {
		t.Error("a Slack scope problem disabled iMessage read sync too")
	}
}
