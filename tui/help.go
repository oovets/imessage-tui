package tui

import (
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
				"Left/Right   Move between panes (Left from pane → chat list)",
				"Ctrl+Up/Down Move focus between stacked panes",
				"Up/Down j/k  Navigate chats or (in list) scroll",
				"g / G        Top / bottom of chat list",
				"Enter        Open chat / send message",
				"Shift+Enter  Newline in input",
			},
		},
		{
			title: "Chat list",
			lines: []string{
				"/            Filter chats (Esc to clear)",
				"d / D        Delete selected chat / confirm delete",
				"r            Rename selected chat",
				"Ctrl+D/R     Delete / rename active pane chat",
				"Ctrl+Left/Right  Resize chat list",
			},
		},
		{
			title: "Messages",
			lines: []string{
				"PgUp/PgDn    Scroll message history",
				"End / G      Jump to newest messages",
				"/img #N      Open image on message row N",
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
			title: "Display",
			lines: []string{
				"Ctrl+S       Toggle chat list",
				"Ctrl+T       Toggle timestamps",
				"Ctrl+N       Toggle line numbers",
				"Ctrl+B       Toggle sender names",
				"Ctrl+E       Toggle pane dividers",
				"Ctrl+P       Toggle chat previews",
				"?            Toggle this help",
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
	body.WriteString(lipgloss.NewStyle().Foreground(ColorWindowPlaceholder).Render("Press ? or Esc to close"))

	content := lipgloss.NewStyle().Padding(1, 2).Render(body.String())
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorChatListSelectedBackground).
		Background(lipgloss.Color("235")).
		Render(content)

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
