package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// noticeApp is an app on two networks with the chat list hidden, one empty
// pane focused, a chat on each network in the list, and a recorder in place
// of the system's notifications.
func noticeApp(t *testing.T) (AppModel, *[]notice) {
	t.Helper()
	app := twoNetworkApp(t)
	app.chatList.SetChats([]models.Chat{
		{GUID: "im-1", DisplayName: "Family"},
		{GUID: "sl:acme:C1", DisplayName: "Engineering"},
	})
	app.showChatList = false
	app.focused = focusWindow
	app.updateLayout()
	alerts := &[]notice{}
	app.alert = func(_ notifyMode, n notice) error {
		*alerts = append(*alerts, n)
		return nil
	}
	return app, alerts
}

// cmdMessages runs a command tree and returns what it produced. Nothing in it
// may block: the test apps have no stream to wait on.
func cmdMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, cmdMessages(child)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

var deliveries int

// deliver hands the app a realtime message from sender and returns the
// toast it raised, or "".
func deliver(t *testing.T, app AppModel, chatGUID, sender, text string, fromMe bool) (AppModel, string) {
	t.Helper()
	deliveries++
	msg := models.Message{
		GUID:        fmt.Sprintf("m%d", deliveries),
		ChatGUID:    chatGUID,
		Text:        text,
		IsFromMe:    fromMe,
		DateCreated: int64(1000 + deliveries),
	}
	if !fromMe {
		msg.Handle = &models.Handle{Address: "x", DisplayName: sender}
	}
	model, cmd := app.Update(providerEventMsg(provider.Event{Kind: provider.EventNewMessage, ChatGUID: chatGUID, Message: msg}))
	app = *model.(*AppModel)
	toast := ""
	for _, m := range cmdMessages(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	return app, toast
}

func showChatInFocusedPane(app *AppModel, chatGUID string) {
	app.windowManager.ShowChat(app.windowManager.FocusedWindow(), &models.Chat{GUID: chatGUID})
}

// A message raises a toast only where nothing else on screen shows it.
func TestNewMessageToast(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, app *AppModel)
		chat   string
		sender string
		text   string
		fromMe bool
		want   string
	}{
		{name: "list hidden, chat in no pane",
			chat: "im-1", sender: "Family", text: "middag?", want: "● Family: middag?"},
		{name: "chat in a pane on screen",
			setup: func(_ *testing.T, app *AppModel) { showChatInFocusedPane(app, "im-1") },
			chat:  "im-1", sender: "Family", text: "middag?"},
		{name: "list shows the chat",
			setup: func(_ *testing.T, app *AppModel) { app.showChatList = true },
			chat:  "im-1", sender: "Family", text: "middag?"},
		{name: "list filtered to another network",
			setup: func(_ *testing.T, app *AppModel) {
				app.showChatList = true
				app.chatList.SetNetwork("slack:acme")
			},
			chat: "im-1", sender: "Family", text: "middag?", want: "● Family: middag?"},
		{name: "chat only in a background tab",
			setup: func(t *testing.T, app *AppModel) {
				showChatInFocusedPane(app, "im-1")
				*app = press(t, *app, altKey('t'))
			},
			chat: "im-1", sender: "Family", text: "middag?", want: "● Family: middag?"},
		{name: "ignored chat",
			setup: func(_ *testing.T, app *AppModel) { app.chatList.ToggleIgnore("im-1") },
			chat:  "im-1", sender: "Family", text: "middag?"},
		{name: "own message",
			chat: "im-1", text: "middag?", fromMe: true},
		{name: "sender is not the chat, text on several lines",
			chat: "sl:acme:C1", sender: "Anna", text: "hej\n\nalla", want: "● Engineering · Anna: hej alla"},
		{name: "chat not in the list yet",
			chat: "im-new", sender: "Bosse", text: "tjena", want: "● Bosse: tjena"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := noticeApp(t)
			if tt.setup != nil {
				tt.setup(t, &app)
			}
			app.updateLayout()
			_, got := deliver(t, app, tt.chat, tt.sender, tt.text, tt.fromMe)
			if got != tt.want {
				t.Errorf("toast = %q, want %q", got, tt.want)
			}
		})
	}
}

// A bell or desktop notification goes out while the terminal is not looked
// at, or, before it has said either way, for a chat that is not on screen.
func TestNotificationsFollowTerminalFocus(t *testing.T) {
	tests := []struct {
		name   string
		mode   notifyMode
		focus  tea.Msg // what the terminal last reported, or nil for nothing
		inPane bool
		want   int
	}{
		{"off", notifyOff, tea.BlurMsg{}, false, 0},
		{"terminal in the background, chat on screen", notifyDesktop, tea.BlurMsg{}, true, 1},
		{"terminal in front, chat off screen", notifyDesktop, tea.FocusMsg{}, false, 0},
		{"terminal back in front", notifyBell, tea.FocusMsg{}, true, 0},
		{"never reported, chat on screen", notifyBell, nil, true, 0},
		{"never reported, chat off screen", notifyBell, nil, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, alerts := noticeApp(t)
			app.notifyMode = tt.mode
			if tt.inPane {
				showChatInFocusedPane(&app, "im-1")
			}
			if tt.name == "terminal back in front" {
				model, _ := app.Update(tea.BlurMsg{})
				app = model.(AppModel)
			}
			if tt.focus != nil {
				model, _ := app.Update(tt.focus)
				app = model.(AppModel)
			}
			deliver(t, app, "im-1", "Family", "middag?", false)
			if got := len(*alerts); got != tt.want {
				t.Errorf("%d notifications, want %d", got, tt.want)
			}
		})
	}
}

// A burst of messages in one chat is one notification; another chat still
// gets its own. The notice names the chat, and the sender when that differs.
func TestNotificationCooldownIsPerChat(t *testing.T) {
	app, alerts := noticeApp(t)
	app.notifyMode = notifyDesktop
	model, _ := app.Update(tea.BlurMsg{})
	app = model.(AppModel)

	app, _ = deliver(t, app, "sl:acme:C1", "Anna", "hej <3 & välkommen", false)
	app, _ = deliver(t, app, "sl:acme:C1", "Anna", "är ni där?", false)
	deliver(t, app, "im-1", "Family", "middag?", false)

	if len(*alerts) != 2 {
		t.Fatalf("%d notifications, want 2: %+v", len(*alerts), *alerts)
	}
	first := (*alerts)[0]
	if first.title() != "Engineering" || first.body() != "Anna: hej <3 & välkommen" {
		t.Errorf("notice = %q / %q", first.title(), first.body())
	}
	if (*alerts)[1].title() != "Family" || (*alerts)[1].body() != "middag?" {
		t.Errorf("second notice = %q / %q", (*alerts)[1].title(), (*alerts)[1].body())
	}
}

func TestNotificationTextIsMadeSafe(t *testing.T) {
	if got, want := oneLine("a\x1b[31mb\n\nc\td\x07"), "a[31mb c d"; got != want {
		t.Errorf("oneLine = %q, want %q", got, want)
	}
	if got, want := escapeNotifyMarkup("<3 & <b>x</b>"), "&lt;3 &amp; &lt;b&gt;x&lt;/b&gt;"; got != want {
		t.Errorf("escapeNotifyMarkup = %q, want %q", got, want)
	}
	for in, want := range map[string]notifyMode{
		"": notifyOff, "off": notifyOff, "Bell": notifyBell, " desktop ": notifyDesktop, "loud": notifyOff,
	} {
		if got := parseNotifyMode(in); got != want {
			t.Errorf("parseNotifyMode(%q) = %v, want %v", in, got, want)
		}
	}
}

// With the list hidden, the top-right corner names the newest unread chat
// and counts the rest, over the panes or beside the tab bar, and the frame
// keeps its exact size.
func TestUnreadBadgeWhileTheListIsHidden(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	app, _ := noticeApp(t)
	top := func(want string) {
		t.Helper()
		rows := strings.Split(app.View(), "\n")
		if len(rows) != app.height {
			t.Errorf("frame is %d rows, want %d", len(rows), app.height)
		}
		for i, row := range rows {
			if w := lipgloss.Width(row); w != app.frameWidth() {
				t.Errorf("row %d is %d columns, want %d: %q", i, w, app.frameWidth(), stripANSI(row))
			}
		}
		first := strings.TrimRight(stripANSI(rows[0]), " ")
		switch {
		case want == "" && strings.Contains(first, "●"):
			t.Errorf("top row has a badge: %q", first)
		case want != "" && !strings.HasSuffix(first, want):
			t.Errorf("top row = %q, want it to end in %q", first, want)
		}
	}

	top("")
	app.chatList.MarkNewMessage("im-1")
	top("● Family")
	app.chatList.MarkNewMessage("sl:acme:C1")
	top("● Engineering +1")

	// Names long enough that the bar needs the whole row: it must make room
	// for the badge rather than run under it.
	app = press(t, app, altKey('t'))
	app.windowManager.SetActiveTabName(strings.Repeat("Andra fliken ", 6))
	app = press(t, app, altKey('1'))
	app.windowManager.SetActiveTabName(strings.Repeat("Första fliken ", 6))
	top("● Engineering +1")
	bar := stripANSI(strings.Split(app.View(), "\n")[0])
	infos := app.windowManager.Tabs()
	labels, _ := tabBarLabels(infos, app.tabTitles(infos), app.tabBarWidth())
	for _, label := range labels {
		if !strings.Contains(bar, label) {
			t.Errorf("badge covers tab %q: %q", label, bar)
		}
	}

	app.showChatList = true
	app.updateLayout()
	top("")
}

// Clicking the badge opens the list on the unread chat, lifting a network
// filter that hides it.
func TestClickingTheBadgeShowsTheUnreadChat(t *testing.T) {
	app, _ := noticeApp(t)
	app.chatList.SetNetwork("slack:acme")
	app.chatList.MarkNewMessage("im-1")

	app = click(t, app, app.frameWidth()-2, 0)
	if !app.showChatList || app.focused != focusChatList {
		t.Fatalf("list shown %v, focus %v", app.showChatList, app.focused)
	}
	if chat := app.chatList.SelectedChat(); chat == nil || chat.GUID != "im-1" {
		t.Errorf("selected %v, want im-1", chat)
	}
	if got := app.chatList.Network(); got != "" {
		t.Errorf("filter still %q", got)
	}
}

// alt+n goes to the newest unread chat: to the pane showing it, in whichever
// tab, or into the focused pane.
func TestJumpToUnread(t *testing.T) {
	app, _ := noticeApp(t)
	msgs := cmdMessages(app.jumpToUnread())
	if len(msgs) != 1 || msgs[0].(toastMsg).text != "Nothing unread" {
		t.Errorf("with nothing unread: %v", msgs)
	}

	showChatInFocusedPane(&app, "sl:acme:C1")
	app = press(t, app, altKey('t'))
	app.chatList.MarkNewMessage("sl:acme:C1")
	app = press(t, app, altKey('n'))
	if app.windowManager.ActiveTab() != 0 {
		t.Fatalf("alt+n stayed on tab %d", app.windowManager.ActiveTab())
	}
	if w := app.windowManager.FocusedWindow(); w.Chat == nil || w.Chat.GUID != "sl:acme:C1" || app.focused != focusWindow {
		t.Errorf("alt+n focused %v", w.Chat)
	}
	if app.chatList.NewestUnread() != nil {
		t.Error("the chat is still unread after going to it")
	}

	app = press(t, app, altKey('2'))
	app.chatList.MarkNewMessage("im-1")
	app = press(t, app, altKey('n'))
	if app.windowManager.ActiveTab() != 1 {
		t.Errorf("alt+n left the tab for a chat in no pane")
	}
	if w := app.windowManager.FocusedWindow(); w.Chat == nil || w.Chat.GUID != "im-1" {
		t.Errorf("alt+n did not open the chat in the focused pane: %v", w.Chat)
	}
}
