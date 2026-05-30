package tui

import (
	"sort"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// emoticon maps a classic text smiley to an emoji glyph.
type emoticon struct {
	text  string
	glyph string
}

// emoticonList is every supported emoticon, longest first so that ":-)" wins
// over ":)" when both could match.
//
// The set is deliberately limited to emoticons whose tail can never be the
// start of a (lowercase) emoji shortcode — punctuation or UPPERCASE letters —
// so inline conversion never blocks shortcode typing like ":pizza". Lowercase
// letter smileys (":p", ":o", ":d") are intentionally omitted for that reason.
var emoticonList []emoticon

func init() {
	raw := map[string]string{
		":)": "🙂", ":-)": "🙂",
		":(": "🙁", ":-(": "🙁",
		":D": "😄", ":-D": "😄",
		";)": "😉", ";-)": "😉",
		":P": "😛", ":-P": "😛",
		":O": "😮", ":-O": "😮",
		":*": "😘", ":-*": "😘",
		":/": "😕", ":-/": "😕",
		":|": "😐", ":-|": "😐",
		":'(": "😢",
		">:(": "😠",
		"<3":  "❤️",
		"</3": "💔",
		"xD":  "😆", "XD": "😆",
	}
	for text, glyph := range raw {
		emoticonList = append(emoticonList, emoticon{text: text, glyph: glyph})
	}
	sort.Slice(emoticonList, func(i, j int) bool {
		return len(emoticonList[i].text) > len(emoticonList[j].text)
	})
}

// replaceEmoticonAtCursor converts a just-typed emoticon immediately before the
// cursor into its emoji glyph. It returns true if a replacement happened.
func (m *InputModel) replaceEmoticonAtCursor() bool {
	before, ok := m.lineBeforeCursor()
	if !ok || len(before) == 0 {
		return false
	}
	for _, e := range emoticonList {
		n := len([]rune(e.text)) // emoticons are ASCII, so rune len == byte len
		if len(before) < n {
			continue
		}
		if string(before[len(before)-n:]) != e.text {
			continue
		}
		// Require a boundary (line start or whitespace) before the emoticon so
		// we don't mangle URLs like http:// or glue onto the end of a word.
		if len(before) > n && !unicode.IsSpace(before[len(before)-n-1]) {
			continue
		}
		for i := 0; i < n; i++ {
			m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		m.textarea.InsertString(e.glyph)
		return true
	}
	return false
}
