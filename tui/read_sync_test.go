package tui

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// markingProvider counts MarkRead calls, which is what reaches Slack's own
// apps as read state.
type markingProvider struct {
	recordingProvider

	marksMu sync.Mutex
	marks   []string
}

func (p *markingProvider) MarkRead(chatGUID string) error {
	p.marksMu.Lock()
	defer p.marksMu.Unlock()
	p.marks = append(p.marks, chatGUID)
	return nil
}

func (p *markingProvider) markCount() int {
	p.marksMu.Lock()
	defer p.marksMu.Unlock()
	return len(p.marks)
}

// markReplies runs a command tree and feeds the mark-read answers in it back
// into the app. Children that block (the stream wait) are left running.
func markReplies(t *testing.T, app *AppModel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	out := make(chan tea.Msg, 16)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				if child != nil {
					go run(child)
				}
			}
			return
		}
		out <- msg
	}
	go run(cmd)
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case msg := <-out:
			switch msg.(type) {
			case markReadSuccessMsg, markReadErrMsg:
				model, next := app.Update(msg)
				*app = asApp(model)
				markReplies(t, app, next)
			}
		case <-deadline:
			return
		}
	}
}

// asApp takes Update's model back, which some paths return by pointer.
func asApp(model tea.Model) AppModel {
	if p, ok := model.(*AppModel); ok {
		return *p
	}
	return model.(AppModel)
}

func slackReadingApp(t *testing.T) (*markingProvider, AppModel, models.Chat) {
	t.Helper()
	slack := &markingProvider{recordingProvider: recordingProvider{id: "slack:acme"}}
	registry := provider.NewRegistry(&recordingProvider{id: "imessage"})
	registry.Register("sl:acme:", slack)
	app := NewAppModelWithConfig(registry, nil, nil)

	chat := models.Chat{GUID: "sl:acme:C1", DisplayName: "#general"}
	app.chatList.SetChats([]models.Chat{chat})
	app.windowManager.FocusedWindow().SetChat(&chat)
	app.focused = focusWindow
	return slack, app, chat
}

func slackMsg(ts string, at int64) models.Message {
	return models.Message{
		GUID:        "sl:acme:C1:" + ts,
		Text:        "hej",
		DateCreated: at,
		ChatGUID:    "sl:acme:C1",
		Handle:      &models.Handle{DisplayName: "Anna"},
	}
}

// Opening a chat marks it read before its history is in, and Slack marks by
// the newest message it has seen — so the first open used to mark nothing.
// The history arriving for the chat being read has to mark it.
func TestLoadedHistoryMarksTheFocusedChatRead(t *testing.T) {
	slack, app, chat := slackReadingApp(t)

	loaded := messagesLoadedMsg{chatGUID: chat.GUID, messages: []models.Message{slackMsg("1700000100.000100", 1700000100000)}}
	model, cmd := app.Update(loaded)
	app = asApp(model)
	markReplies(t, &app, cmd)
	if got := slack.markCount(); got != 1 {
		t.Fatalf("marks after first load = %d, want 1", got)
	}

	// The pane reloads on a timer. Nothing new, nothing to mark.
	model, cmd = app.Update(loaded)
	app = asApp(model)
	markReplies(t, &app, cmd)
	if got := slack.markCount(); got != 1 {
		t.Errorf("marks after an unchanged reload = %d, want still 1", got)
	}
}

// A message arriving in the chat being read is read; Slack has to hear so.
func TestIncomingMessageInTheFocusedChatMarksItRead(t *testing.T) {
	slack, app, chat := slackReadingApp(t)

	model, cmd := app.Update(providerEventMsg(provider.Event{
		Kind:     provider.EventNewMessage,
		ChatGUID: chat.GUID,
		Message:  slackMsg("1700000200.000100", 1700000200000),
	}))
	app = asApp(model)
	markReplies(t, &app, cmd)
	if got := slack.markCount(); got != 1 {
		t.Errorf("marks = %d, want 1", got)
	}
}

// With the focus on the chat list, a message in the open pane has not been
// read yet and must stay unread in Slack.
func TestIncomingMessageWithoutFocusLeavesItUnread(t *testing.T) {
	slack, app, chat := slackReadingApp(t)
	app.focused = focusChatList

	model, cmd := app.Update(providerEventMsg(provider.Event{
		Kind:     provider.EventNewMessage,
		ChatGUID: chat.GUID,
		Message:  slackMsg("1700000300.000100", 1700000300000),
	}))
	app = asApp(model)
	markReplies(t, &app, cmd)
	if got := slack.markCount(); got != 0 {
		t.Errorf("marks = %d, want 0", got)
	}
}

// A mark asked for while one is in flight is kept, not dropped: otherwise the
// messages that arrived meanwhile stay unread in Slack.
func TestMarkAskedForInFlightRunsAfterwards(t *testing.T) {
	slack, app, chat := slackReadingApp(t)
	app.windowManager.SetCachedMessages(chat.GUID, []models.Message{slackMsg("1700000100.000100", 1700000100000)})

	first := app.markChatReadIfNeeded(chat.GUID)
	if second := app.markChatReadIfNeeded(chat.GUID); second != nil {
		t.Fatal("a second mark ran while the first was in flight")
	}
	markReplies(t, &app, first)
	if got := slack.markCount(); got != 2 {
		t.Errorf("marks = %d, want 2", got)
	}
}
