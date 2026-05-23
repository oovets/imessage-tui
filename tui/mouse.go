package tui

import tea "github.com/charmbracelet/bubbletea"

func mouseIsWheel(msg tea.MouseMsg) bool {
	return msg.Button == tea.MouseButtonWheelUp ||
		msg.Button == tea.MouseButtonWheelDown ||
		msg.Button == tea.MouseButtonWheelLeft ||
		msg.Button == tea.MouseButtonWheelRight
}
