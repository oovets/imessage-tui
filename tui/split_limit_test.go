package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
)

// splitToMaxPanes opens panes until the manager refuses, splitting a different
// pane each time the way a user would — splitting only the newest pane halves
// the same rectangle over and over and runs out of room long before the limit.
func splitToMaxPanes(t *testing.T, wm *WindowManager) error {
	t.Helper()
	direction := SplitHorizontal
	for range maxPanes * 2 {
		if err := wm.SplitWindow(direction); err != nil {
			return err
		}
		// Alternate the axis and move focus to the oldest pane, so the tree
		// grows in breadth instead of chasing one shrinking corner.
		if direction == SplitHorizontal {
			direction = SplitVertical
		} else {
			direction = SplitHorizontal
		}
		windows := wm.AllWindows()
		if len(windows) > 0 {
			wm.SetFocus(windows[0].ID)
		}
	}
	return nil
}

func TestEightPanesCanBeOpen(t *testing.T) {
	wm := NewWindowManager()
	wm.SetSize(200, 60)

	err := splitToMaxPanes(t, wm)
	if !errors.Is(err, ErrTooManyPanes) {
		t.Fatalf("splitting stopped with %v, want the pane limit", err)
	}
	if got := len(wm.AllWindows()); got != maxPanes {
		t.Errorf("opened %d panes, want %d", got, maxPanes)
	}
	if maxPanes != 8 {
		t.Errorf("maxPanes = %d, want 8", maxPanes)
	}
}

// Raising the limit must not let the frame outgrow the terminal — the failure
// mode that bends dividers and pushes everything sideways.
func TestEightPanesStayInsideTheTerminal(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	for _, size := range []struct{ width, height int }{
		{80, 24},
		{120, 40},
		{200, 60},
	} {
		app := NewAppModelWithConfig(nil, nil, nil)
		chat := models.Chat{GUID: "chat-a", DisplayName: "Familjen 🎉"}
		app.chatList.SetChats([]models.Chat{chat})
		model, _ := app.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		app = model.(AppModel)

		if err := splitToMaxPanes(t, app.windowManager); !errors.Is(err, ErrTooManyPanes) {
			t.Fatalf("%dx%d: %v", size.width, size.height, err)
		}

		// A freshly split pane has no chat yet — the state you are in between
		// pressing ctrl+f and picking a conversation — and its placeholder is
		// the block most likely to outgrow its share.
		app.updateLayout()
		assertFrameFits(t, app, size.width, size.height, "empty panes")

		for _, window := range app.windowManager.AllWindows() {
			window.SetChat(&chat)
			window.Messages.SetMessages([]models.Message{
				{GUID: "m1", Text: "hej 👨‍👩‍👧‍👦 där", DateCreated: 1000,
					Handle: &models.Handle{DisplayName: "Anna"}},
			})
		}
		app.updateLayout()
		assertFrameFits(t, app, size.width, size.height, "panes with messages")
	}
}

func assertFrameFits(t *testing.T, app AppModel, width, height int, what string) {
	t.Helper()
	view := app.View()
	if got := lipgloss.Height(view); got != height {
		t.Errorf("%dx%d, %s: rendered %d rows, want %d", width, height, what, got, height)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("%dx%d, %s: row %d is %d columns wide", width, height, what, i, got)
		}
	}
}

// Every pane stays reachable by keyboard, or the extra panes are only useful
// with a mouse.
func TestEveryPaneIsReachableByKeyboard(t *testing.T) {
	wm := NewWindowManager()
	wm.SetSize(200, 60)
	if err := splitToMaxPanes(t, wm); !errors.Is(err, ErrTooManyPanes) {
		t.Fatalf("splitting stopped with %v", err)
	}

	// Tab cycles in id order and deliberately stops at the last pane, where it
	// hands focus back to the chat list. Starting from the first pane, it must
	// still visit every one of them.
	windows := wm.AllWindows()
	first := windows[0].ID
	for _, window := range windows {
		if window.ID < first {
			first = window.ID
		}
	}
	wm.SetFocus(first)

	seen := map[WindowID]struct{}{first: {}}
	for wm.CycleFocus() {
		seen[wm.FocusedWindow().ID] = struct{}{}
	}
	if len(seen) != maxPanes {
		t.Errorf("cycling reached %d of %d panes", len(seen), maxPanes)
	}
}

func TestSplitBeforeLayoutIsAllowed(t *testing.T) {
	wm := NewWindowManager()
	if err := wm.SplitWindow(SplitHorizontal); err != nil {
		t.Fatalf("split before any layout pass was refused: %v", err)
	}
}
