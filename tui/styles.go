package tui

import (
	"github.com/charmbracelet/lipgloss"
)

const (
	ChatListWidth = 25  // fixed width for left panel
	InputHeight   = 3   // input box + borders

	// Window dividers
	DividerVertical   = "│"
	DividerHorizontal = "─"
)

// Color scheme
const (
	ColorPrimary   = lipgloss.Color("212")  // pink
	ColorSecondary = lipgloss.Color("86")   // green
	ColorAccent    = lipgloss.Color("242")  // gray
	ColorBorder    = lipgloss.Color("240")  // dark gray
)

var (
	// Panel styles (no borders, just padding)
	PanelStyle = lipgloss.NewStyle().
		Padding(0, 1)

	ActivePanelStyle = lipgloss.NewStyle().
		Padding(0, 1)

	// Chat list styles
	ChatListItemStyle = lipgloss.NewStyle().
		Padding(0).
		Margin(0)

	ChatListItemSelectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(ColorPrimary).
		Padding(0).
		Margin(0)

	// Message styles
	MyMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Dark: "86", Light: "22"})

	TheirMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Dark: "252", Light: "232"}).
		Align(lipgloss.Left)

	TimestampStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Dark: "242", Light: "240"}).
		PaddingRight(1)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("235")).
		Padding(0, 1)

	// Input styles (no border)
	InputStyle = lipgloss.NewStyle()

	// Window styles for split view (no borders)
	FocusedWindowStyle = lipgloss.NewStyle().
		Padding(0, 1)

	UnfocusedWindowStyle = lipgloss.NewStyle().
		Padding(0, 1)
)

// CalculateLayout returns the optimal dimensions for each panel
func CalculateLayout(screenWidth, screenHeight int) (chatListWidth, messagesWidth, messagesHeight, inputHeight int) {
	chatListWidth = ChatListWidth
	messagesWidth = screenWidth - chatListWidth - 2 // -2 for padding
	messagesHeight = screenHeight - InputHeight - 1 // -1 status bar
	inputHeight = InputHeight

	return
}
