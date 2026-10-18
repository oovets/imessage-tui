package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

const (
	testRootGUID   = "sl:acme:C1:100.1"
	testReplyGUID  = "sl:acme:C1:150.1"
	testThreadGUID = testRootGUID
)

// threadingProvider is a backend whose messages carry threads: both messages
// of #general belong to the one thread rooted at testRootGUID.
type threadingProvider struct {
	*recordingProvider
}

var _ provider.Threader = threadingProvider{}

func (threadingProvider) Thread(messageGUID string) (models.Chat, error) {
	switch messageGUID {
	case testRootGUID, testReplyGUID:
		return models.Chat{GUID: testThreadGUID, DisplayName: "#general › thread"}, nil
	}
	return models.Chat{}, errors.New("not a message here")
}

func threadApp(t *testing.T) (*recordingProvider, AppModel) {
	t.Helper()
	imessage := &recordingProvider{id: "imessage"}
	slack := &recordingProvider{id: "slack:acme"}
	registry := provider.NewRegistry(imessage)
	registry.Register("sl:acme:", threadingProvider{slack})
	app := NewAppModelWithConfig(registry, nil, nil)
	app.width, app.height = 120, 40
	app.updateLayout()

	general := models.Chat{GUID: "sl:acme:C1", DisplayName: "#general"}
	app.chatList.SetChats([]models.Chat{general})
	window := app.windowManager.FocusedWindow()
	app.windowManager.ShowChat(window, &general)
	window.Messages.SetMessages([]models.Message{
		{GUID: testRootGUID, ChatGUID: general.GUID, Text: "rot", DateCreated: 1},
		{GUID: testReplyGUID, ChatGUID: general.GUID, Text: "↳ svar", DateCreated: 2},
	})
	app.focused = focusWindow
	window.Input.Focus()
	return slack, app
}

func typeCommand(t *testing.T, app AppModel, text string) (AppModel, tea.Cmd) {
	t.Helper()
	app.windowManager.FocusedWindow().Input.textarea.SetValue(text)
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model.(AppModel), cmd
}

func TestThreadCommandOpensTheThreadInAPaneBeside(t *testing.T) {
	slack, app := threadApp(t)
	channel := app.windowManager.FocusedWindow()

	app, cmd := typeCommand(t, app, "/t #2")
	if app.err != nil {
		t.Fatalf("error: %v", app.err)
	}
	if n := app.windowManager.WindowCount(); n != 2 {
		t.Fatalf("got %d panes, want the channel and the thread", n)
	}
	thread := app.windowManager.FocusedWindow()
	if thread == channel {
		t.Fatal("focus stayed on the channel pane")
	}
	if thread.Chat == nil || thread.Chat.GUID != testThreadGUID {
		t.Fatalf("focused pane shows %+v, want the thread", thread.Chat)
	}
	if channel.Chat == nil || channel.Chat.GUID != "sl:acme:C1" {
		t.Errorf("channel pane now shows %+v", channel.Chat)
	}
	if got := channel.Input.GetText(); got != "" {
		t.Errorf("command left in the composer: %q", got)
	}
	// The thread splits off side by side, like Slack's thread panel.
	if app.windowManager.root.Direction != SplitHorizontal {
		t.Errorf("split direction = %v, want side by side", app.windowManager.root.Direction)
	}
	if !thread.Input.Focused() {
		t.Error("the thread pane cannot be typed in")
	}

	if cmd == nil {
		t.Fatal("expected the thread to be loaded")
	}
	go runTestCmd(cmd)
	waitFor(t, func() bool {
		slack.mu.Lock()
		defer slack.mu.Unlock()
		for _, guid := range slack.asked {
			if guid == testThreadGUID {
				return true
			}
		}
		return false
	})
}

func TestThreadCommandFocusesAThreadAlreadyOpen(t *testing.T) {
	_, app := threadApp(t)
	channel := app.windowManager.FocusedWindow()
	app, _ = typeCommand(t, app, "/t #2")
	thread := app.windowManager.FocusedWindow()

	app.windowManager.SetFocus(channel.ID)
	app, _ = typeCommand(t, app, "/t #1")
	if app.err != nil {
		t.Fatalf("error: %v", app.err)
	}
	if n := app.windowManager.WindowCount(); n != 2 {
		t.Errorf("got %d panes, want the open thread reused", n)
	}
	if app.windowManager.FocusedWindow() != thread {
		t.Error("focus did not move to the pane already showing the thread")
	}
}

func TestThreadCommandReportsWhatItCannotOpen(t *testing.T) {
	tests := []struct {
		name    string
		chat    models.Chat
		message string
		command string
	}{
		{"a backend without threads", models.Chat{GUID: "chat-a"}, "imsg-1", "/t #1"},
		{"a row that is not there", models.Chat{GUID: "sl:acme:C1"}, testRootGUID, "/t #5"},
		{"a message the backend rejects", models.Chat{GUID: "sl:acme:C1"}, "sl:acme:C1:pending", "/t #1"},
		{"no row number", models.Chat{GUID: "sl:acme:C1"}, testRootGUID, "/t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, app := threadApp(t)
			window := app.windowManager.FocusedWindow()
			app.windowManager.ShowChat(window, &tt.chat)
			window.Messages.SetMessages([]models.Message{{GUID: tt.message, ChatGUID: tt.chat.GUID, Text: "hej", DateCreated: 1}})

			app, _ = typeCommand(t, app, tt.command)
			if app.err == nil {
				t.Error("no error shown")
			}
			if n := app.windowManager.WindowCount(); n != 1 {
				t.Errorf("got %d panes, want none opened", n)
			}
		})
	}
}

func TestParseThreadCommand(t *testing.T) {
	tests := []struct {
		raw     string
		target  int
		handled bool
		wantErr bool
	}{
		{"/t #3", 3, true, false},
		{"/t 3", 3, true, false},
		{"  /t #12  ", 12, true, false},
		{"/t", 0, true, true},
		{"/t #0", 0, true, true},
		{"/t #x", 0, true, true},
		{"/t #3 text", 0, true, true},
		{"/tu", 0, false, false},
		{"/top", 0, false, false},
		{"hej /t #3", 0, false, false},
	}
	for _, tt := range tests {
		target, handled, err := parseThreadCommand(tt.raw)
		if target != tt.target || handled != tt.handled || (err != nil) != tt.wantErr {
			t.Errorf("parseThreadCommand(%q) = %d, %v, %v; want %d, %v, err %v",
				tt.raw, target, handled, err, tt.target, tt.handled, tt.wantErr)
		}
	}
}

// A thread is not in the chat list, so the restore has to ask the backend for
// it or the pane comes back empty.
func TestThreadPaneComesBackAfterARestart(t *testing.T) {
	_, app := threadApp(t)
	app.didRestoreLayout = false
	app.savedLayoutState = &config.LayoutState{
		Root: &config.LayoutNodeState{
			Direction:  int(SplitHorizontal),
			SplitRatio: 0.5,
			Left:       &config.LayoutNodeState{},
			Right:      &config.LayoutNodeState{},
		},
		LeafChatGUIDs:    []string{"sl:acme:C1", testThreadGUID},
		FocusedLeafIndex: 1,
	}

	if !app.restoreLayoutFromState([]models.Chat{{GUID: "sl:acme:C1", DisplayName: "#general"}}) {
		t.Fatal("layout not restored")
	}
	thread := app.windowManager.FocusedWindow()
	if thread.Chat == nil || thread.Chat.GUID != testThreadGUID {
		t.Fatalf("thread pane shows %+v, want the thread", thread.Chat)
	}
	if thread.Chat.DisplayName != "#general › thread" {
		t.Errorf("thread pane is called %q", thread.Chat.DisplayName)
	}
}
