package tui

import (
	"github.com/charmbracelet/lipgloss"
)

const (
	DefaultChatListWidth = 25 // default width for left panel
	MinChatListWidth     = 10
	MaxChatListWidth     = 80
	ChatListResizeStep   = 2
	InputHeight          = 3 // input box + borders

	// Window dividers
	DividerVertical   = "│"
	DividerHorizontal = "─"
)

// Shared palette values.
//
// Nothing here switches on the terminal background. Detection
// (lipgloss.HasDarkBackground) queries the terminal over stdin and is wrong
// often enough to be unusable — multiplexers answer for the terminal, some
// emulators never reply, and a wrong answer paints unreadable text. Every
// value below is instead picked to keep usable contrast against *both* a white
// and a black background, and message body text carries no color at all so it
// inherits the terminal's own foreground, which is correct by construction.
const (
	PaletteBlack = lipgloss.Color("0")
	PalettePink  = lipgloss.Color("212") // #ff87d7, used behind black text only
	PaletteBlue  = lipgloss.Color("32")  // #0087d7, ~3.9:1 on white / ~5.4:1 on black
	PaletteGray  = lipgloss.Color("243") // #767676, ~4.6:1 on white and on black
	PaletteDim   = lipgloss.Color("242") // #6c6c6c, one step dimmer for dividers
	PaletteRed   = lipgloss.Color("196")
)

// Semantic colors by UI area.
const (
	ColorChatListSelectedForeground = PaletteBlack
	ColorChatListSelectedBackground = PalettePink
	ColorChatListNewMessage         = PaletteRed
	ColorWindowPlaceholder          = PaletteGray
	ColorWindowDivider              = PaletteDim
	ColorMuted                      = PaletteGray
	ColorAccent                     = PaletteBlue
)

var (
	// Panel styles (no borders, just padding)
	PanelStyle = lipgloss.NewStyle().
			Padding(0, 1)

	ActivePanelStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(ColorChatListSelectedBackground)

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
			Foreground(ColorAccent)

	// Incoming messages deliberately set no foreground: the terminal's own
	// default foreground is the only value guaranteed to contrast with the
	// terminal's own background.
	TheirMessageStyle = lipgloss.NewStyle().
				Align(lipgloss.Left)

	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			PaddingRight(1)

	// Transient status line. Reverse video inverts whatever the terminal's own
	// colors are, so the line reads as a bar on light and dark alike without
	// committing to a foreground/background pair.
	StatusLineStyle = lipgloss.NewStyle().
			Reverse(true).
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
	chatListWidth = DefaultChatListWidth
	messagesWidth = screenWidth - chatListWidth - 2 // -2 for padding
	messagesHeight = screenHeight - InputHeight - 1 // -1 status bar
	inputHeight = InputHeight

	return
}
