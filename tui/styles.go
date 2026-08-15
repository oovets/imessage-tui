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

// Brighter variants, used only when the background is *known* to be dark
// because the user said so (BB_THEME=dark). The compromise values above have
// to stay legible on white too, which caps how bright they can be; once the
// background is a given, the ceiling lifts and the same roles can be tuned for
// black. These are never selected by auto-detection — see ApplyTheme.
const (
	PaletteBlueBright = lipgloss.Color("39")  // #00afff, ~8.6:1 on black
	PaletteGrayBright = lipgloss.Color("245") // #8a8a8a, ~6.1:1 on black
	PaletteDimBright  = lipgloss.Color("243") // #767676, ~4.6:1 on black
)

// nickPalette colours sender names so a group conversation can be read by who
// is talking rather than by reading every name.
//
// Every entry clears 3.8:1 against white *and* 4.2:1 against black, measured on
// the xterm 256 palette — the same both-backgrounds rule the rest of the
// palette follows. Hues are spread so neighbours stay apart for the common
// colour-vision deficiencies, and three colours are deliberately absent: the
// accent blue (outgoing messages), the selection pink, and red (the
// new-message marker), so a name can never be mistaken for one of those.
var nickPalette = []lipgloss.Color{
	lipgloss.Color("28"),  // #008700 green
	lipgloss.Color("30"),  // #008787 teal
	lipgloss.Color("62"),  // #5f5fd7 indigo
	lipgloss.Color("98"),  // #875fd7 violet
	lipgloss.Color("163"), // #d700af magenta
	lipgloss.Color("161"), // #d7005f rose
	lipgloss.Color("130"), // #af5f00 orange
	lipgloss.Color("100"), // #878700 olive
}

// nickPaletteDark is the same eight hues, brightened for a background that is
// known to be dark. Same rule as the rest of the palette: only an explicitly
// pinned theme gets these, never auto-detection.
var nickPaletteDark = []lipgloss.Color{
	lipgloss.Color("40"),  // #00d700 green
	lipgloss.Color("44"),  // #00d7d7 teal
	lipgloss.Color("105"), // #8787ff indigo
	lipgloss.Color("141"), // #af87ff violet
	lipgloss.Color("207"), // #ff5fd7 magenta
	lipgloss.Color("204"), // #ff5f87 rose
	lipgloss.Color("214"), // #ffaf00 orange
	lipgloss.Color("184"), // #d7d700 olive
}

// NickPalette is the palette in force. Read at render time, so swapping it is
// all ApplyTheme has to do.
var NickPalette = nickPalette

// Semantic colors by UI area. Variables rather than constants so ApplyTheme can
// swap in the tuned palette; the values here are the background-agnostic
// defaults every code path gets unless the user pins a theme.
var (
	ColorChatListSelectedForeground = PaletteBlack
	ColorChatListSelectedBackground = PalettePink
	ColorChatListNewMessage         = PaletteRed
	ColorWindowPlaceholder          = PaletteGray
	ColorWindowDivider              = PaletteDim
	ColorMuted                      = PaletteGray
	ColorAccent                     = PaletteBlue
)

// ApplyTheme tunes the palette for a background the user has pinned explicitly
// with BB_THEME / theme:. Only an explicit setting reaches here — auto-detection
// stays on the compromise palette, because a wrong guess about the background
// paints unreadable text and detection is wrong often enough to matter.
//
// Call it before constructing any model: the chat list and the message styles
// read these values when they are built, not when they render.
func ApplyTheme(dark bool) {
	if dark {
		ColorAccent = PaletteBlueBright
		ColorMuted = PaletteGrayBright
		ColorWindowPlaceholder = PaletteGrayBright
		ColorWindowDivider = PaletteDimBright
		NickPalette = nickPaletteDark
	} else {
		ColorAccent = PaletteBlue
		ColorMuted = PaletteGray
		ColorWindowPlaceholder = PaletteGray
		ColorWindowDivider = PaletteDim
		NickPalette = nickPalette
	}
	rebuildStyles()
}

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

// rebuildStyles re-derives the package-level styles that bake a palette value
// in at construction. Styles that read a color at render time pick up a theme
// switch on their own; these do not, so every one that references a Color*
// variable belongs here.
func rebuildStyles() {
	ActivePanelStyle = ActivePanelStyle.BorderForeground(ColorChatListSelectedBackground)
	ChatListItemSelectedStyle = ChatListItemSelectedStyle.
		Foreground(ColorChatListSelectedForeground).
		Background(ColorChatListSelectedBackground)
	MyMessageStyle = MyMessageStyle.Foreground(ColorAccent)
	TimestampStyle = TimestampStyle.Foreground(ColorMuted)
}

// CalculateLayout returns the optimal dimensions for each panel
func CalculateLayout(screenWidth, screenHeight int) (chatListWidth, messagesWidth, messagesHeight, inputHeight int) {
	chatListWidth = DefaultChatListWidth
	messagesWidth = screenWidth - chatListWidth - 2 // -2 for padding
	messagesHeight = screenHeight - InputHeight - 1 // -1 status bar
	inputHeight = InputHeight

	return
}
