package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
)

// terminalCells is how many columns iTerm2, Terminal.app and most other
// terminals move the cursor for s: one cell per grapheme cluster, sized by the
// cluster's first character, with VS16 changing nothing. That is narrower than
// lipgloss's count for ❤️ and the like, and the gap between the two is what
// bent the pane dividers.
func terminalCells(s string) int {
	cells := 0
	for _, cluster := range graphemeClusters(ansi.Strip(s)) {
		first := []rune(cluster)[0]
		cells += ansi.StringWidth(string(first))
	}
	return cells
}

func graphemeClusters(s string) []string {
	var out []string
	for len(s) > 0 {
		cluster, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		out = append(out, cluster)
		s = s[len(cluster):]
	}
	return out
}

// dividerColumns lists the terminal columns a row draws a vertical divider in.
func dividerColumns(row string) []int {
	var cols []int
	col := 0
	for _, cluster := range graphemeClusters(ansi.Strip(row)) {
		if cluster == DividerVertical {
			cols = append(cols, col)
		}
		col += terminalCells(cluster)
	}
	return cols
}

var wideTextCases = []string{
	"❤️",
	"ja❤️",
	"yes❤️❤️",
	"❤️ ja mamma säger ju samma sak så har ingen aning",
	"😂",
	"hitta inte? 😂",
	"🥰",
	"😅",
	"👍🏽",
	"👨‍👩‍👧",
	"日本語のテキスト",
	"✔️ klart",
	"☺️",
	"1️⃣ 2️⃣",
	"❤️‍🔥",
	"🏳️‍🌈",
	"hej 👍 då",
}

func TestSettledEmojiTakeAsManyTerminalCellsAsLipglossMeasured(t *testing.T) {
	for _, text := range wideTextCases {
		styled := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(text)
		for _, in := range []string{text, styled} {
			out := settleEmojiWidths(in)
			want := lipgloss.Width(in)
			if got := lipgloss.Width(out); got != want {
				t.Errorf("settling %q changed its measured width from %d to %d", in, want, got)
			}
			if got := terminalCells(out); got != want {
				t.Errorf("%q takes %d terminal cells after settling, want the %d lipgloss laid out", in, got, want)
			}
		}
	}
}

func TestSettlingLeavesTextWithoutVariationSelectorsAlone(t *testing.T) {
	for _, in := range []string{
		"plain text",
		"😂 👍🏽 👨‍👩‍👧 日本",
		"\x1b[1;31mfet röd\x1b[0m",
		"\x1b]8;;https://example.com\x1b\\länk\x1b]8;;\x1b\\",
	} {
		if out := settleEmojiWidths(in); out != in {
			t.Errorf("settleEmojiWidths(%q) = %q, want it unchanged", in, out)
		}
	}
}

func TestSettlingKeepsEscapeSequencesAroundEmoji(t *testing.T) {
	in := "\x1b[31m❤️\x1b[0m slut"
	want := "\x1b[31m❤ \x1b[0m slut"
	if got := settleEmojiWidths(in); got != want {
		t.Errorf("settleEmojiWidths(%q) = %q, want %q", in, got, want)
	}
}

// The reported frame: two chats side by side, rows full of hearts and faces.
// On screen every row must be exactly as wide as the frame lipgloss laid out
// and draw the dividers in the same columns as every other row.
func TestPaneDividersStayStraightOnRowsWithEmoji(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	for _, size := range []struct{ width, height int }{{120, 40}, {97, 33}, {61, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			app := NewAppModelWithConfig(nil, nil, nil)
			model, _ := app.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			app = model.(AppModel)
			if err := app.windowManager.SplitWindow(SplitHorizontal); err != nil {
				t.Fatal(err)
			}

			var mine, theirs []models.Message
			for i, text := range wideTextCases {
				mine = append(mine, models.Message{
					GUID: fmt.Sprintf("me-%d", i), Text: text, IsFromMe: true, DateCreated: int64(1000 + i),
				})
				theirs = append(theirs, models.Message{
					GUID: fmt.Sprintf("nora-%d", i), Text: text, DateCreated: int64(1000 + i),
					ReactionCounts: map[string]int{"❤️": 1},
				})
			}
			windows := app.windowManager.AllWindows()
			for i, w := range windows {
				chat := models.Chat{GUID: fmt.Sprintf("chat-%d", i), DisplayName: "Nora ❤️"}
				w.SetChat(&chat)
				if i == 0 {
					w.Messages.SetMessages(mine)
				} else {
					w.Messages.SetMessages(theirs)
				}
			}
			windows[0].Input.textarea.SetValue("skickar ❤️❤️ och 👍🏽")
			windows[0].Input.reflowHeight()

			frame := app.View()
			frameWidth := lipgloss.Width(frame)
			rows := strings.Split(frame, "\n")
			if len(rows) != size.height {
				t.Errorf("frame has %d rows, want %d", len(rows), size.height)
			}
			wantDividers := dividerColumns(rows[0])
			if len(wantDividers) == 0 {
				t.Fatalf("first row draws no divider: %q", ansi.Strip(rows[0]))
			}
			for i, row := range rows {
				if got := terminalCells(row); got != frameWidth {
					t.Errorf("row %d takes %d terminal cells, want %d: %q", i, got, frameWidth, ansi.Strip(row))
				}
				if got := lipgloss.Width(row); got != frameWidth {
					t.Errorf("row %d measures %d columns, want %d: %q", i, got, frameWidth, ansi.Strip(row))
				}
				if got := dividerColumns(row); fmt.Sprint(got) != fmt.Sprint(wantDividers) {
					t.Errorf("row %d draws dividers at %v, want %v: %q", i, got, wantDividers, ansi.Strip(row))
				}
			}
		})
	}
}
