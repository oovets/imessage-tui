package irc

import (
	"strings"
	"unicode"

	"github.com/ergochat/irc-go/ircfmt"
	"github.com/oovets/imessage-tui/models"
)

// ctcpDelim wraps a CTCP request inside a PRIVMSG. ACTION — /me — is the only
// one people read; the rest (VERSION, PING, TIME) are machines talking.
const ctcpDelim = "\x01"

// parseCTCP splits a CTCP body into its command and argument. ok is false for
// text that is not CTCP at all.
func parseCTCP(text string) (command, arg string, ok bool) {
	if !strings.HasPrefix(text, ctcpDelim) {
		return "", "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, ctcpDelim), ctcpDelim)
	command, arg, _ = strings.Cut(body, " ")
	return strings.ToUpper(command), arg, true
}

// convertText turns a line's IRC formatting into the mrkdwn the renderer
// already draws for Slack, and says whether it did.
//
// Raw control codes must never reach the pane: \x03 and friends take no
// columns in some terminals and a column in others, and a BEL would ring the
// user's terminal for every message someone pastes it into. Bold, italic,
// strikethrough and monospace have a mark to become; colour, underline and
// reverse have none and are dropped, keeping their text.
//
// A message with no formatting stays plain text, so that an asterisk someone
// typed is shown as an asterisk rather than read as a mark.
func convertText(raw string) (string, models.TextFormat) {
	chunks := ircfmt.Split(raw)
	formatted := false
	for i := range chunks {
		if hasMark(&chunks[i]) {
			formatted = true
			break
		}
	}
	if !formatted {
		var b strings.Builder
		for _, chunk := range chunks {
			b.WriteString(sanitize(chunk.Content))
		}
		return b.String(), ""
	}

	var b strings.Builder
	for i := range chunks {
		chunk := &chunks[i]
		content := sanitize(chunk.Content)
		if !hasMark(chunk) {
			b.WriteString(content)
			continue
		}
		// A mark opens only where a word starts and closes only where one
		// ends, so the spaces around the formatted words stay outside it.
		inner := strings.TrimSpace(content)
		if inner == "" {
			b.WriteString(content)
			continue
		}
		start := strings.Index(content, inner)
		b.WriteString(content[:start])
		b.WriteString(wrapMarks(chunk, inner))
		b.WriteString(content[start+len(inner):])
	}
	return b.String(), models.FormatMrkdwn
}

// hasMark reports whether a chunk carries formatting that has a mrkdwn mark.
func hasMark(chunk *ircfmt.FormattedSubstring) bool {
	return chunk.Bold || chunk.Italic || chunk.Strikethrough || chunk.Monospace
}

func wrapMarks(chunk *ircfmt.FormattedSubstring, text string) string {
	if chunk.Monospace {
		// Nothing is read inside code, so other marks would show as written.
		return "`" + text + "`"
	}
	if chunk.Strikethrough {
		text = "~" + text + "~"
	}
	if chunk.Italic {
		text = "_" + text + "_"
	}
	if chunk.Bold {
		text = "*" + text + "*"
	}
	return text
}

// sanitize drops the control characters formatting leaves behind and any
// others a line can carry. A tab becomes a space, since the pane measures
// columns and a tab has none of its own.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f:
			return -1
		case unicode.Is(unicode.Cc, r):
			return -1
		default:
			return r
		}
	}, s)
}

// splitForWire cuts a message into lines the server will take. IRC carries
// one line per PRIVMSG, so newlines split it, and a line longer than max bytes
// is cut at the last space before the limit — or, in one long word, at the
// last rune boundary. Pieces keep their bytes exactly, so joining a line's
// pieces gives the line back. Blank lines have nothing to send and are left
// out.
func splitForWire(text string, max int) []string {
	if max < 16 {
		max = 16
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var pieces []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for len(line) > max {
			cut := max
			for cut > 0 && !runeStart(line[cut]) {
				cut--
			}
			if space := strings.LastIndexByte(line[:cut], ' '); space > max/2 {
				cut = space + 1
			}
			pieces = append(pieces, line[:cut])
			line = line[cut:]
		}
		if line != "" {
			pieces = append(pieces, line)
		}
	}
	return pieces
}

func runeStart(b byte) bool { return b&0xC0 != 0x80 }
