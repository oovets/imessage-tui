package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Nick completion, as WeeChat does it: Tab completes the word before the
// cursor to a nick, Tab again moves on to the next match and Shift+Tab back.
// A nick that starts the message gets ": " after it — the way IRC addresses
// someone — and one anywhere else a space. Matching ignores case, and the
// order is WeeChat's "speakers" order: whoever spoke most recently first,
// then everyone else in the channel alphabetically.

// nickCompleter is the suffix after a nick that starts the message.
const nickCompleter = ": "

// nickCompletion is a completion in progress: Tab pressed and nothing else
// typed since.
type nickCompletion struct {
	matches  []string
	index    int
	inserted string // what the last Tab put in place of the typed word
}

// CompleteNick completes the word before the cursor from candidates, or moves
// to the next (backward: previous) match when the last key was a completion.
// handled says the key was used: it is true whenever there is a word to
// complete, even with no match, so that Tab in the middle of a word never
// jumps focus away from what is being typed. With no word before the cursor
// it is false, and Tab keeps its usual meaning.
func (m *InputModel) CompleteNick(candidates []string, backward bool) bool {
	before, ok := m.lineBeforeCursor()
	if !ok {
		return false
	}

	if nc := m.nickCompletion; nc != nil && strings.HasSuffix(string(before), nc.inserted) && len(nc.matches) > 0 {
		step := 1
		if backward {
			step = -1
		}
		nc.index = (nc.index + step + len(nc.matches)) % len(nc.matches)
		replaced := len([]rune(nc.inserted))
		text := m.completedText(nc.matches[nc.index], len(before)-replaced)
		m.replaceBeforeCursor(replaced, text)
		nc.inserted = text
		return true
	}
	m.nickCompletion = nil

	start := len(before)
	for start > 0 && isNickRune(before[start-1]) {
		start--
	}
	word := string(before[start:])
	if word == "" {
		return false
	}

	prefix := strings.ToLower(word)
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), prefix) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return true
	}
	index := 0
	if backward {
		index = len(matches) - 1
	}
	text := m.completedText(matches[index], start)
	m.replaceBeforeCursor(len([]rune(word)), text)
	m.nickCompletion = &nickCompletion{matches: matches, index: index, inserted: text}
	return true
}

// completedText is a nick as it is inserted at rune column start: addressed
// with ": " when it begins the message, followed by a space otherwise.
func (m *InputModel) completedText(nick string, start int) string {
	if start == 0 && m.textarea.Line() == 0 {
		return nick + nickCompleter
	}
	return nick + " "
}

// replaceBeforeCursor deletes n runes before the cursor and inserts text.
// Going through the textarea's own Update keeps its cursor and wrapping
// consistent, as the emoji autocomplete does.
func (m *InputModel) replaceBeforeCursor(n int, text string) {
	for i := 0; i < n; i++ {
		m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m.textarea.InsertString(text)
	m.reflowHeight()
}

// recentSpeakers are the nicks that wrote in the pane, most recent first,
// each once — the front of WeeChat's completion order.
func (m *MessagesModel) recentSpeakers() []string {
	seen := make(map[string]struct{})
	var nicks []string
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.IsFromMe || msg.Handle == nil || msg.Handle.DisplayName == "" {
			continue
		}
		key := strings.ToLower(msg.Handle.DisplayName)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		nicks = append(nicks, msg.Handle.DisplayName)
	}
	return nicks
}

// nickCandidates are the nicks Tab completes to in a pane: recent speakers
// first, then the rest of the channel, never the user's own.
func (m *AppModel) nickCandidates(window *ChatWindow, self string) []string {
	seen := map[string]struct{}{strings.ToLower(self): {}}
	var out []string
	add := func(nick string) {
		key := strings.ToLower(nick)
		if _, dup := seen[key]; dup || nick == "" {
			return
		}
		seen[key] = struct{}{}
		out = append(out, nick)
	}
	for _, nick := range window.Messages.recentSpeakers() {
		add(nick)
	}
	for _, nick := range m.providers.Nicks(window.Chat.GUID) {
		add(nick)
	}
	return out
}

// completeNickKey runs Tab or Shift+Tab as nick completion in the focused
// pane when its chat is on a nick network. handled is false where Tab is not
// completion, so it keeps its usual meaning.
func (m *AppModel) completeNickKey(msg tea.KeyMsg) bool {
	key := msg.String()
	if (key != "tab" && key != "shift+tab") || m.focused != focusWindow {
		return false
	}
	window := m.windowManager.FocusedWindow()
	if window == nil || window.Chat == nil || !window.Input.Focused() {
		return false
	}
	self := m.providers.SelfNick(window.Chat.GUID)
	if self == "" {
		return false
	}
	return window.Input.CompleteNick(m.nickCandidates(window, self), key == "shift+tab")
}
