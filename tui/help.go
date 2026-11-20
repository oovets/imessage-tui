package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func helpOverlayView(width, height int) string {
	if width < 20 || height < 10 {
		return ""
	}

	sections := []struct {
		title string
		lines []string
	}{
		{
			title: "Navigation",
			lines: []string{
				"Tab          Toggle chat list / active pane",
				"Esc          Focus chat list",
				"Shift+←/→    Move between panes (Left from pane → chat list)",
				"Ctrl+Up/Down Move focus between stacked panes",
				"←/→ ↑/↓      Move the cursor while writing; navigate otherwise",
				"Up/Down j/k  Navigate chats or (in list) scroll",
				"g / G        Top / bottom of chat list",
				"Enter        Open chat / send message",
				"Ctrl+K       Find a chat from anywhere (Enter opens it)",
				"Shift+Enter  Newline in input",
			},
		},
		{
			title: "Chat list",
			lines: []string{
				"/            Find a chat: ↑/↓ pick, Enter opens, Esc closes",
				"PgUp/PgDn    Page through chats (or Shift+↑/↓, mouse wheel)",
				"n / N        Next / previous network (all → each network)",
				"Click        A network chip or header: show only that network",
				"Double-click A chat: open it in the active pane",
				"s            Sort by recent / group by network",
				"p            Pin / unpin selected chat",
				"K / J        Move a pinned chat up / down",
				"m            Mute / unmute selected chat (replies to you notify)",
				"x            Ignore / stop ignoring selected chat",
				"I            Show / hide ignored chats",
				"U            Show / hide chats from unknown senders",
				"t            Show / hide times in the chat list",
				"d / D        Delete selected chat / confirm delete",
				"r            Rename selected chat",
				"Ctrl+D/R     Delete / rename active pane chat",
				"Ctrl+O       Pin / unpin active pane chat",
				"Ctrl+Left/Right  Resize chat list",
			},
		},
		{
			title: "Messages",
			lines: []string{
				"PgUp/PgDn    Scroll message history (or Shift+↑/↓)",
				"End / G      Jump to newest messages",
				"<3 :) ;) 8)  Become emojis as you type",
				":D :P xD :/  Become emojis once you type a space or send",
				"/img #N      Open image on message row N",
				"/open        Open the newest link in the pane",
				"/open #N [K] Open link K (default 1) on message row N",
				"Click a URL  Open it in the browser",
				"Click ↱      Jump to the message a reply answers",
				"/r #N text   Reply to message row N (Slack: in its thread)",
				"/t #N        Open row N's thread in a new pane (Slack)",
				"/filter ...  Hide lines in IRC chats (/filter list, as WeeChat)",
				"/ignore ...  Hide a nick in IRC chats for good",
				"/h /lol      React to latest message",
				"/tu /te      Thumbs up / down latest message",
			},
		},
		{
			title: "Windows",
			lines: []string{
				"Ctrl+F       Split pane horizontally",
				"Ctrl+G       Split pane vertically",
				"Ctrl+W       Close focused pane",
				"Ctrl+Shift+←/→  Adjust split ratio",
			},
		},
		{
			title: "Tabs",
			lines: []string{
				"Alt+T        New tab, with its own panes",
				"Alt+W        Close tab (its chats stay in the list)",
				"Alt+1…9      Go to tab N",
				"Alt+, / .    Previous / next tab",
				"Alt+R        Name tab (empty: name it after its networks)",
				"Alt+N        Go to the newest unread chat",
				"Click        A tab on the bar: show it",
			},
		},
		{
			title: "Display",
			lines: []string{
				"Ctrl+S       Toggle chat list",
				"Ctrl+T       Toggle timestamps",
				"Ctrl+N       Toggle line numbers",
				"Ctrl+B       Toggle sender names (this chat, remembered)",
				"Alt+M        Sender names in 1:1 chats too",
				"Alt+= / -    Filtering off/on: everywhere / this pane's chat",
				"Ctrl+E       Toggle pane dividers",
				"Ctrl+P       Toggle chat previews",
				"F1           Toggle this help",
				"q Ctrl+C     Quit",
			},
		},
	}

	var body strings.Builder
	body.WriteString(lipgloss.NewStyle().Bold(true).Render("Keyboard shortcuts"))
	body.WriteString("\n\n")
	for _, sec := range sections {
		body.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorChatListSelectedBackground).Render(sec.title))
		body.WriteString("\n")
		for _, line := range sec.lines {
			body.WriteString("  ")
			body.WriteString(line)
			body.WriteString("\n")
		}
		body.WriteString("\n")
	}
	note := lipgloss.NewStyle().Foreground(ColorWindowPlaceholder)
	body.WriteString(note.Render("Single-letter shortcuts (? q d r n s p m K J x I U t g G /) and the plain arrow keys pause"))
	body.WriteString("\n")
	body.WriteString(note.Render("while you are typing in a message or in chat search — there they edit"))
	body.WriteString("\n")
	body.WriteString(note.Render("text. Ctrl/Alt shortcuts keep working mid-message."))
	body.WriteString("\n\n")
	body.WriteString(note.Render("Press ? or Esc to close"))

	content := lipgloss.NewStyle().Padding(1, 2).Render(body.String())
	// No explicit background: the help view replaces the whole screen, so it
	// does not need one, and a hardcoded dark fill would swallow the text on a
	// light-background terminal.
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorChatListSelectedBackground).
		Render(content)

	return centerOverlay(box, width, height)
}

// centerOverlay places a bordered box in the middle of a width × height
// screen, which it replaces.
func centerOverlay(box string, width, height int) string {
	boxWidth := lipgloss.Width(box)
	boxHeight := lipgloss.Height(box)
	if boxWidth > width-2 {
		boxWidth = width - 2
	}
	if boxHeight > height-2 {
		boxHeight = height - 2
	}

	padTop := (height - boxHeight) / 2
	if padTop < 0 {
		padTop = 0
	}
	padLeft := (width - boxWidth) / 2
	if padLeft < 0 {
		padLeft = 0
	}

	row := lipgloss.NewStyle().Width(width).Render(strings.Repeat(" ", padLeft) + box)
	if padTop > 0 {
		blank := lipgloss.NewStyle().Width(width).Render("")
		rows := make([]string, padTop)
		for i := range rows {
			rows[i] = blank
		}
		return lipgloss.JoinVertical(lipgloss.Left, append(rows, row)...)
	}
	return row
}

// textOverlayView shows lines in a box like the help's, for listings a
// command asks for (/filter list). Lines that do not fit are counted rather
// than drawn, so the box never runs off the screen.
func textOverlayView(title string, lines []string, width, height int) string {
	if width < 20 || height < 10 {
		return ""
	}
	room := height - 9
	if room < 1 {
		room = 1
	}
	shown := lines
	more := 0
	if len(shown) > room {
		more = len(shown) - room + 1
		shown = shown[:room-1]
	}
	maxLine := width - 10
	var body strings.Builder
	body.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	body.WriteString("\n\n")
	for _, line := range shown {
		body.WriteString(truncateToWidth(line, maxLine))
		body.WriteString("\n")
	}
	note := lipgloss.NewStyle().Foreground(ColorWindowPlaceholder)
	if more > 0 {
		body.WriteString(note.Render(fmt.Sprintf("… %d more", more)))
		body.WriteString("\n")
	}
	body.WriteString("\n")
	body.WriteString(note.Render("Press any key to close"))

	content := lipgloss.NewStyle().Padding(1, 2).Render(body.String())
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorChatListSelectedBackground).
		Render(content)
	return centerOverlay(box, width, height)
}
