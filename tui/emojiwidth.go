package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const variationSelector16 = '️'

// settleEmojiWidths rewrites a finished frame so every emoji takes as many
// cells in the terminal as lipgloss measured for it while laying the frame
// out.
//
// Lipgloss counts a character followed by VS16 (U+FE0F), such as ❤️ ✔️ ☺️ or
// a keycap, as one two-column emoji. iTerm2, Terminal.app, Alacritty and most
// other terminals ignore the selector and size the cell by the base character
// alone, which for these is one column. Every such emoji then makes its row
// one column shorter on screen than it was padded for, so the divider and the
// pane to its right slide left on exactly the rows that hold one.
//
// Measuring differently is not an option: lipgloss pads, truncates and joins
// panes with its own width function. Instead the selector is dropped and the
// column it was worth is written out as a space. A bare base character is one
// column in every terminal, kitty and Ghostty included, so the frame now lines
// up regardless of which terminal draws it, and all the layout and mouse
// columns computed against lipgloss widths stay valid.
func settleEmojiWidths(frame string) string {
	if !strings.ContainsRune(frame, variationSelector16) {
		return frame
	}

	var b strings.Builder
	b.Grow(len(frame) + 16)
	var state byte
	for len(frame) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(frame, state, nil)
		if n <= 0 {
			b.WriteString(frame)
			break
		}
		// Text, not an escape sequence. A keycap's selector arrives with zero
		// width of its own, split from the digit before it, and goes as well.
		printable := seq[0] >= 0x20 && seq[0] != 0x7f
		if printable && strings.ContainsRune(seq, variationSelector16) {
			bare := strings.ReplaceAll(seq, string(variationSelector16), "")
			b.WriteString(bare)
			b.WriteString(strings.Repeat(" ", max(0, width-ansi.StringWidth(bare))))
		} else {
			b.WriteString(seq)
		}
		state = newState
		frame = frame[n:]
	}
	return b.String()
}
