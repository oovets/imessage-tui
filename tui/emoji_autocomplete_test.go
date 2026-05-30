package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeInput(s string) InputModel {
	m := NewInputModel()
	m.Focus() // textarea ignores key input while blurred
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestEmojiAutocompleteAcceptReplacesToken(t *testing.T) {
	m := typeInput("hello :smil")
	if !m.AutocompleteActive() {
		t.Fatalf("expected popup active after ':smil'")
	}
	if m.acMatches[0].name != "smile" {
		t.Fatalf("expected 'smile' first, got %q", m.acMatches[0].name)
	}

	handled, _ := m.HandleAutocompleteKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatalf("enter should be handled by the popup")
	}
	if got := m.GetText(); got != "hello 😄" {
		t.Fatalf("unexpected text after accept: %q", got)
	}
	if m.AutocompleteActive() {
		t.Fatalf("popup should be dismissed after accept")
	}
}

func TestEmojiAutocompleteDoesNotTriggerMidWord(t *testing.T) {
	// A ':' that is not word-initial (e.g. a clock time) must not open the popup.
	if typeInput("10:30").AutocompleteActive() {
		t.Fatalf("'10:30' should not trigger the popup")
	}
	// Below the minimum query length, nothing shows yet.
	if typeInput(":a").AutocompleteActive() {
		t.Fatalf("single-char query should not trigger the popup")
	}
}

func TestEmojiAutocompleteNavigationWraps(t *testing.T) {
	m := typeInput(":fire")
	if !m.AutocompleteActive() || len(m.acMatches) < 2 {
		t.Fatalf("':fire' should trigger with multiple matches")
	}
	last := len(m.acMatches) - 1
	m.moveAutocomplete(-1) // wrap from first to last
	if m.acSelected != last {
		t.Fatalf("expected wrap to %d, got %d", last, m.acSelected)
	}
	m.moveAutocomplete(1) // wrap back to first
	if m.acSelected != 0 {
		t.Fatalf("expected wrap to 0, got %d", m.acSelected)
	}
}
