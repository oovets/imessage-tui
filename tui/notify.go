package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/oovets/imessage-tui/models"
)

// A message can arrive where nobody sees it: in a chat in no pane while the
// chat list is hidden or filtered to another network, or while the terminal
// is behind another window. This file is how the app says so — a badge and a
// toast inside it, and, when configured, a bell or a desktop notification
// outside it.

// notifyMode is the "notifications" setting: how a message is announced
// beyond the app's own screen.
type notifyMode int

const (
	notifyOff notifyMode = iota
	notifyBell
	notifyDesktop
)

func parseNotifyMode(s string) notifyMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off", "none", "false":
		return notifyOff
	case "bell":
		return notifyBell
	case "desktop":
		return notifyDesktop
	}
	log.Printf("notifications: unknown setting %q, leaving them off (off, bell or desktop)", s)
	return notifyOff
}

// terminalFocus is what the terminal last said about its window. Terminals
// report it only once asked (tea.WithReportFocus), only on a change, and some
// never do — so until the first report it is unknown, not focused.
type terminalFocus int

const (
	terminalFocusUnknown terminalFocus = iota
	terminalFocused
	terminalBlurred
)

const (
	// noticeToastDuration is how long a message's toast stays up.
	noticeToastDuration = 4 * time.Second
	// alertCooldown is how long a chat that has just rung or notified stays
	// quiet, so a burst of messages is one notification, not ten.
	alertCooldown = 30 * time.Second
	// notifyTimeout bounds the notification command: notify-send waits on
	// D-Bus, which has no daemon to answer on some desktops.
	notifyTimeout = 5 * time.Second
	// noticeTextWidth caps a notification's text; the toast is cut to the
	// screen anyway.
	noticeTextWidth = 200
)

// ReportsFocus says whether the program should ask the terminal for focus
// reports. Only notifications need them, and every report is one more escape
// sequence on the input that can arrive torn.
func (m AppModel) ReportsFocus() bool {
	return m.notifyMode != notifyOff
}

// notice is a message as it is announced.
type notice struct {
	chat   string // the chat's name
	sender string // who wrote, when that is not just the chat's name
	text   string
}

func (n notice) title() string {
	return n.chat
}

func (n notice) body() string {
	if n.sender != "" {
		return n.sender + ": " + n.text
	}
	return n.text
}

func (n notice) toast() string {
	head := n.chat
	if n.sender != "" {
		head += " · " + n.sender
	}
	return "● " + head + ": " + n.text
}

// noticeFor names a message's chat and sender for a notice.
func (m *AppModel) noticeFor(msg models.Message) notice {
	sender := senderName(msg)
	chatName := ""
	if chat, ok := m.chatList.ChatByGUID(msg.ChatGUID); ok {
		chatName = chat.GetDisplayName()
	} else if showing := m.windowManager.WindowsShowingChat(msg.ChatGUID); len(showing) > 0 {
		chatName = showing[0].Chat.GetDisplayName()
	}
	n := notice{chat: oneLine(chatName), text: oneLine(messagePreview(msg))}
	switch {
	case n.chat == "":
		n.chat = oneLine(sender)
	case sender != "Unknown" && !strings.EqualFold(strings.TrimSpace(sender), strings.TrimSpace(chatName)):
		n.sender = oneLine(sender)
	}
	if n.text == "" {
		n.text = "New message"
	}
	n.text = truncateToWidth(n.text, noticeTextWidth)
	return n
}

// oneLine flattens text for a single row: whitespace runs, newlines included,
// become one space, and control characters go — an escape inside a message
// must not reach the terminal as one.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// announce tells about a new message that may otherwise pass unseen.
//
// Inside the app, a toast when the chat is nowhere on screen: in no pane of
// the tab shown, and on no row of the list, which is hidden or filtered to
// another network. Outside it, with notifications on, a bell or desktop
// notification when the terminal is not being looked at — or, from a terminal
// that has never said whether it is, when the chat is not on screen.
func (m *AppModel) announce(msg models.Message) []tea.Cmd {
	if msg.IsFromMe || msg.ChatGUID == "" || m.chatList.Quiet(msg.ChatGUID) {
		return nil
	}
	onScreen := len(m.windowManager.ActiveWindowsShowingChat(msg.ChatGUID)) > 0 ||
		(m.showChatList && m.chatList.Listed(msg.ChatGUID))
	n := m.noticeFor(msg)
	var cmds []tea.Cmd
	if !onScreen {
		cmds = append(cmds, showToastCmd(n.toast(), noticeToastDuration))
	}
	if m.shouldAlert(msg.ChatGUID, onScreen) {
		cmds = append(cmds, alertCmd(m.alert, m.notifyMode, n))
	}
	return cmds
}

// shouldAlert decides whether a message rings or notifies outside the app,
// and starts the chat's cooldown when it does.
func (m *AppModel) shouldAlert(chatGUID string, onScreen bool) bool {
	switch {
	case m.notifyMode == notifyOff || m.alert == nil:
		return false
	case m.terminalFocus == terminalFocused:
		return false
	case m.terminalFocus == terminalFocusUnknown && onScreen:
		return false
	}
	now := time.Now()
	if last, ok := m.alertedAt[chatGUID]; ok && now.Sub(last) < alertCooldown {
		return false
	}
	m.alertedAt[chatGUID] = now
	return true
}

// alerter delivers a notice outside the app. Tests swap in a recorder.
type alerter func(mode notifyMode, n notice) error

func alertCmd(alert alerter, mode notifyMode, n notice) tea.Cmd {
	return func() tea.Msg {
		if err := alert(mode, n); err != nil {
			log.Printf("notification failed: %v", err)
		}
		return nil
	}
}

// systemAlert is the real alerter.
func systemAlert(mode notifyMode, n notice) error {
	switch mode {
	case notifyBell:
		return ringBell()
	case notifyDesktop:
		return desktopNotify(n.title(), n.body())
	}
	return nil
}

// ringBell rings the terminal bell, which terminals and multiplexers turn
// into an urgency hint or a flagged window. Over SSH it is the one alert that
// reaches the machine in front of you.
//
// It goes to /dev/tty rather than stdout, which Bubble Tea owns, and is the
// one byte the app writes to the terminal itself. That is safe beside a frame
// being written: BEL moves no cursor, and inside a CSI sequence a terminal
// acts on it and carries on with the sequence. Frames hold no OSC sequence,
// the one place BEL would end something early.
func ringBell() error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	_, err = tty.Write([]byte{'\a'})
	return err
}

// desktopNotify shows a desktop notification. The text was written by other
// people, so it goes to the command as arguments, never through a shell, and
// after "--", so a name starting with a dash is not read as an option.
func desktopNotify(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			"--", title, body)
	case "linux", "freebsd", "openbsd", "netbsd":
		cmd = exec.CommandContext(ctx, "notify-send",
			"--app-name=imessage-tui", "--category=im.received",
			"--", title, escapeNotifyMarkup(body))
	default:
		return fmt.Errorf("desktop notifications are not supported on %s", runtime.GOOS)
	}
	return cmd.Run()
}

// escapeNotifyMarkup escapes a notification body for notification daemons,
// which read it as a small subset of HTML: "<3" or "a & b" would otherwise be
// taken as markup, and a daemon that cannot parse it may drop the text.
func escapeNotifyMarkup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// unreadBadge is what the top-right corner says while the chat list is
// hidden and something in it is unread: the newest unread chat, and how many
// more there are. "" when there is nothing to say.
func (m AppModel) unreadBadge() string {
	if m.showChatList || m.width < 20 {
		return ""
	}
	chat := m.chatList.NewestUnread()
	if chat == nil {
		return ""
	}
	text := "● " + truncateToWidth(oneLine(chat.GetDisplayName()), 24)
	if more := m.chatList.NewMessageCount() - 1; more > 0 {
		text += " +" + itoa(more)
	}
	text = truncateToWidth(" "+text+" ", m.width/2)
	return lipgloss.NewStyle().Foreground(ColorChatListNewMessage).Bold(true).Render(text)
}

// unreadBadgeAt reports whether screen cell x, y is on the unread badge.
func (m AppModel) unreadBadgeAt(x, y int) bool {
	w := lipgloss.Width(m.unreadBadge())
	right := m.frameWidth()
	return w > 0 && y == 0 && x >= right-w && x < right
}

// frameWidth is how wide the frame is drawn, which is a little short of the
// terminal: the pane area keeps a margin on its right.
func (m AppModel) frameWidth() int {
	return m.panesX() + m.windowManager.width
}

// overlayTopRight draws s over the right end of view's first row, view being
// width columns wide.
func overlayTopRight(view, s string, width int) string {
	w := lipgloss.Width(s)
	if w == 0 || w > width {
		return view
	}
	first, rest, more := strings.Cut(view, "\n")
	left := ansi.Truncate(first, width-w, "")
	left += strings.Repeat(" ", max(width-w-lipgloss.Width(left), 0))
	row := left + ansi.ResetStyle + s
	if !more {
		return row
	}
	return row + "\n" + rest
}

// revealUnread shows the chat list on the newest unread chat — what a click
// on the badge does. A filter that hides the chat is lifted.
func (m *AppModel) revealUnread() {
	if m.focused == focusWindow {
		if window := m.windowManager.FocusedWindow(); window != nil {
			window.Input.textarea.Blur()
		}
	}
	m.showChatList = true
	m.focused = focusChatList
	if chat := m.chatList.NewestUnread(); chat != nil {
		if !m.chatList.Listed(chat.GUID) {
			m.chatList.SetNetwork("")
		}
		m.chatList.SelectChatByGUID(chat.GUID)
	}
	m.updateLayout()
	m.saveUIState()
}

// jumpToUnread goes to the newest unread chat: to a pane already showing it,
// in another tab if that is where it is, or else into the focused pane.
func (m *AppModel) jumpToUnread() tea.Cmd {
	chat := m.chatList.NewestUnread()
	if chat == nil {
		return showToastCmd("Nothing unread", 2*time.Second)
	}
	if tab, id, ok := m.windowManager.TabShowingChat(chat.GUID); ok {
		switched := m.windowManager.SwitchTab(tab)
		m.windowManager.SetFocus(id)
		m.focused = focusWindow
		if switched {
			return m.afterTabChange()
		}
		m.windowManager.FocusedWindow().Input.textarea.Focus()
		m.saveLayoutState()
		return m.clearFocusedWindowNewMessageIndicator()
	}
	window := m.windowManager.FocusedWindow()
	if window == nil {
		return nil
	}
	m.focused = focusWindow
	window.Input.textarea.Focus()
	return m.selectChat(chat, window)
}
