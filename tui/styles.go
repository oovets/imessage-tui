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

// Shared palette values.
const (
	PaletteBlack      = lipgloss.Color("0")
	PalettePink       = lipgloss.Color("212")
	PaletteGray       = lipgloss.Color("242")
	PaletteDarkGray   = lipgloss.Color("240")
	PaletteRed        = lipgloss.Color("196")
	PaletteStatusFG   = lipgloss.Color("241")
	PaletteStatusBG   = lipgloss.Color("235")
)

// Semantic colors by UI area.
const (
	ColorChatListSelectedForeground = PaletteBlack
	ColorChatListSelectedBackground = PalettePink
	ColorChatListNewMessage         = PaletteRed
	ColorWindowPlaceholder          = PaletteGray
	ColorWindowDivider              = PaletteDarkGray
	ColorStatusBarForeground        = PaletteStatusFG
	ColorStatusBarBackground        = PaletteStatusBG
	ColorMyMessageDark              = "86"
	ColorMyMessageLight             = "22"
	ColorTheirMessageDark           = "252"
	ColorTheirMessageLight          = "232"
	ColorTimestampDark              = "242"
	ColorTimestampLight             = "240"
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
		Foreground(ColorChatListSelectedForeground).
		Background(ColorChatListSelectedBackground).
		Padding(0).
		Margin(0)

	// Message styles
	MyMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Dark: ColorMyMessageDark, Light: ColorMyMessageLight})

	TheirMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Dark: ColorTheirMessageDark, Light: ColorTheirMessageLight}).
		Align(lipgloss.Left)

	TimestampStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Dark: ColorTimestampDark, Light: ColorTimestampLight}).
		PaddingRight(1)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
		Foreground(ColorStatusBarForeground).
		Background(ColorStatusBarBackground).
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
