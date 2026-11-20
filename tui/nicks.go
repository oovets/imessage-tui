package tui

import (
	"hash/fnv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Nick colouring and highlights for chats on a provider.NickNetwork (IRC),
// drawn the way IRC clients draw them — WeeChat's nick colours, its
// highlights, and the colorize_nicks script for nicks inside the text.

// minMentionLength keeps one-letter nicks from colouring every "a" and "I".
const minMentionLength = 2

// nickStyleFrom picks a stable colour for a name from palette.
//
// Stable across restarts and across panes showing the same conversation, which
// rules out anything derived from map iteration or arrival order: the same
// person must keep the same colour or the colour tells you nothing. Matching is
// case-insensitive so a handle that arrives capitalised differently does not
// become a second person.
func nickStyleFrom(name string, palette []lipgloss.Color) lipgloss.Style {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return lipgloss.NewStyle().Foreground(palette[int(hash.Sum32()%uint32(len(palette)))])
}

// nickStyle is a name's colour in the palette every chat uses.
func nickStyle(name string) lipgloss.Style {
	return nickStyleFrom(name, NickPalette)
}

// highlightStyle marks the user's own nick where someone else wrote it.
// Built on use, like quoteStyle, so a theme pinned after startup reaches it.
func highlightStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(PaletteBlack).Background(PaletteHighlight)
}

// isNickRune reports whether r can be part of a nick. RFC 2812 allows letters,
// digits and []\`_^{|}-; letters beyond ASCII are let in too, since networks
// with UTF-8 nicks exist and a name is a name.
func isNickRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("[]\\`_^{|}-", r)
}

// nickWord is a whole word of a line that names someone, by byte offsets
// into the line as it is, escape sequences included.
type nickWord struct {
	start, end int
	key        string // lower case, for looking it up
}

// nickWords finds the words of line that could be nicks: maximal runs of nick
// characters in the text a reader sees. ANSI escape sequences are stepped
// over, so a line the mrkdwn renderer already styled is read by its letters,
// and nothing inside a URL counts: colouring part of a link would change what
// the line says and break the link.
func nickWords(line string) []nickWord {
	type visible struct {
		r          rune
		start, end int
	}
	var chars []visible
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			i = skipEscape(line, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		chars = append(chars, visible{r: r, start: i, end: i + size})
		i += size
	}

	// Which characters belong to a word that is a URL.
	inURL := make([]bool, len(chars))
	for from := 0; from < len(chars); {
		if unicode.IsSpace(chars[from].r) {
			from++
			continue
		}
		to := from
		var field strings.Builder
		for to < len(chars) && !unicode.IsSpace(chars[to].r) {
			field.WriteRune(chars[to].r)
			to++
		}
		if strings.Contains(field.String(), "://") {
			for k := from; k < to; k++ {
				inURL[k] = true
			}
		}
		from = to
	}

	var words []nickWord
	for from := 0; from < len(chars); {
		if !isNickRune(chars[from].r) {
			from++
			continue
		}
		to := from
		var word strings.Builder
		url := false
		for to < len(chars) && isNickRune(chars[to].r) {
			word.WriteRune(chars[to].r)
			url = url || inURL[to]
			to++
		}
		if !url && to-from >= minMentionLength {
			words = append(words, nickWord{start: chars[from].start, end: chars[to-1].end, key: strings.ToLower(word.String())})
		}
		from = to
	}
	return words
}

// skipEscape returns the index just past the escape sequence starting at i:
// a CSI sequence (ESC [ … final byte), or ESC and one more byte otherwise.
func skipEscape(s string, i int) int {
	if i+1 < len(s) && s[i+1] == '[' {
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
	}
	return min(i+2, len(s))
}

// mentionsNick reports whether text names nick as a whole word.
func mentionsNick(text, nick string) bool {
	if len(nick) < minMentionLength {
		return false
	}
	key := strings.ToLower(nick)
	for _, word := range nickWords(text) {
		if word.key == key {
			return true
		}
	}
	return false
}

// decorateNicks colours the nicks named in one line of a message: someone who
// has written in the pane takes the colour of their own lines, and the user's
// own nick (self) is highlighted. Each nick is styled as a word on its own,
// which survives wrapping; the reset after it is re-armed by the caller's
// reopenAfterResets, as for any other styled fragment. highlightSelf is false
// for the user's own messages, where their nick is not news.
func decorateNicks(line string, known map[string]struct{}, self string, highlightSelf bool, palette []lipgloss.Color) string {
	words := nickWords(line)
	if len(words) == 0 {
		return line
	}
	selfKey := strings.ToLower(self)
	var out strings.Builder
	last := 0
	for _, word := range words {
		var style lipgloss.Style
		switch _, isKnown := known[word.key]; {
		case selfKey != "" && word.key == selfKey:
			if !highlightSelf {
				continue
			}
			style = highlightStyle()
		case isKnown:
			style = nickStyleFrom(word.key, palette)
		default:
			continue
		}
		open := styleOpenSequence(style)
		if open == "" {
			// No colour profile (a pipe, a test without one): nothing to add.
			continue
		}
		out.WriteString(line[last:word.start])
		out.WriteString(open)
		out.WriteString(line[word.start:word.end])
		out.WriteString(ansiReset)
		last = word.end
	}
	out.WriteString(line[last:])
	return out.String()
}
