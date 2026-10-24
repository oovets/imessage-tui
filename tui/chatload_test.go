package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// failingChats is a backend whose chat list does not load, like a BlueBubbles
// server that does not answer in time. Everything else it does still works.
type failingChats struct{ *recordingProvider }

func (failingChats) Chats(int) ([]models.Chat, error) {
	return nil, errors.New("context deadline exceeded")
}

// flakyStart is a restart where iMessage's chat list times out and Slack's
// loads. The layout saved before it has a pane on each network, and one on a
// Slack chat that has since been deleted.
func flakyStart(t *testing.T) (AppModel, *recordingProvider, chatsLoadedMsg) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	imessage := &recordingProvider{id: "imessage", chats: []models.Chat{
		{GUID: "im-1", DisplayName: "Family"},
		{GUID: "im-2", DisplayName: "Bob"},
	}}
	slack := &recordingProvider{id: "slack:acme", chats: []models.Chat{
		{GUID: "sl:acme:C1", DisplayName: "#general"},
	}}

	before := twoNetworkApp(t)
	for i, guid := range []string{"im-1", "sl:acme:C1", "sl:acme:GONE"} {
		if i > 0 {
			if err := before.windowManager.SplitWindow(SplitHorizontal); err != nil {
				t.Fatal(err)
			}
		}
		before.windowManager.ShowChat(before.windowManager.FocusedWindow(), &models.Chat{GUID: guid})
	}
	saved, ok := before.layoutState()
	if !ok {
		t.Fatal("no layout to save")
	}

	registry := provider.NewRegistry(failingChats{imessage})
	registry.Register("sl:acme:", slack)
	app := NewAppModelWithConfig(registry, nil, nil)
	app.width, app.height = 120, 30
	app.updateLayout()
	app.savedLayoutState = &saved
	app.windowManager.SetCachedMessages("im-1", []models.Message{{
		GUID: "m1", ChatGUID: "im-1", Text: "middag?", DateCreated: 1,
		Handle: &models.Handle{Address: "+4670", DisplayName: "Mamma"},
	}})
	return app, imessage, loadChatsCmd(registry.All(), 50, 0)().(chatsLoadedMsg)
}

func paneChats(app AppModel) map[string]*ChatWindow {
	out := make(map[string]*ChatWindow)
	for _, w := range app.windowManager.AllWindows() {
		if w.Chat != nil {
			out[w.Chat.GUID] = w
		}
	}
	return out
}

// A network whose chat list times out at startup is asked again until it
// loads. Meanwhile its panes keep their chats, so the layout saved in the
// meantime still has them; a chat that is gone from a network that did load
// leaves its pane empty, as before.
func TestChatsThatFailToLoadAreRetriedAndKeepTheirPanes(t *testing.T) {
	app, imessage, first := flakyStart(t)
	if len(first.failed) != 1 || first.failed[0].ID() != "imessage" {
		t.Fatalf("failed = %v, want imessage", first.failed)
	}

	model, cmd := app.Update(first)
	app = model.(AppModel)
	if cmd == nil {
		t.Fatal("no retry scheduled")
	}
	if app.err == nil || !strings.Contains(app.err.Error(), "imessage chats did not load, retrying") {
		t.Errorf("status line says %v", app.err)
	}
	if got := app.chatList.Len(); got != 1 {
		t.Errorf("list holds %d chats, want Slack's one", got)
	}
	panes := paneChats(app)
	stand, ok := panes["im-1"]
	if !ok {
		t.Fatalf("the iMessage pane came back empty; panes show %v", keys(panes))
	}
	if stand.Chat.GetDisplayName() != "Mamma" || len(stand.Messages.messages) != 1 {
		t.Errorf("stand-in shows %q with %d messages", stand.Chat.GetDisplayName(), len(stand.Messages.messages))
	}
	if _, ok := panes["sl:acme:GONE"]; ok {
		t.Error("a chat missing from a network that loaded kept its pane")
	}
	saved, _ := app.layoutState()
	if !slices.Contains(saved.LeafChatGUIDs, "im-1") {
		t.Errorf("layout saved meanwhile has lost the iMessage pane: %v", saved.LeafChatGUIDs)
	}

	// The retry gets through.
	model, cmd = app.Update(loadChatsCmd([]provider.Provider{imessage}, 50, 1)())
	app = model.(AppModel)
	if cmd != nil {
		t.Error("retrying again after the chats came in")
	}
	if got := app.chatList.Len(); got != 3 {
		t.Errorf("list holds %d chats after the retry, want 3", got)
	}
	pane := paneChats(app)["im-1"]
	if pane == nil || pane.Chat.GetDisplayName() != "Family" || len(pane.Messages.messages) != 1 {
		t.Errorf("pane after the retry: %+v", pane)
	}
	if len(app.unloadedBackends) != 0 {
		t.Errorf("still waiting on %v", app.unloadedBackends)
	}
}

// A retry that fails again keeps trying, without saying so again, and a list
// with nothing in it yet says it is loading rather than "No chats".
func TestRetriesKeepGoingQuietly(t *testing.T) {
	app, imessage, _ := flakyStart(t)
	model, _ := app.Update(loadChatsCmd([]provider.Provider{failingChats{imessage}}, 50, 0)())
	app = model.(AppModel)
	if view := stripANSI(app.chatList.View()); !strings.Contains(view, "Loading chats") {
		t.Errorf("empty list while retrying shows %q", view)
	}

	app.err = nil
	model, cmd := app.Update(loadChatsCmd([]provider.Provider{failingChats{imessage}}, 50, 3)())
	app = model.(AppModel)
	if cmd == nil {
		t.Error("gave up after a failed retry")
	}
	if app.err != nil {
		t.Errorf("a failed retry said %v", app.err)
	}

	for attempt, want := range map[int]time.Duration{1: 5 * time.Second, 2: 15 * time.Second, 5: 2 * time.Minute, 40: 2 * time.Minute} {
		if got := chatRetryDelay(attempt); got != want {
			t.Errorf("retry %d waits %v, want %v", attempt, got, want)
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
