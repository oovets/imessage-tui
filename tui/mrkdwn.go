package tui

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

// Slack's text arrives with its marks in place — *bold*, _italic_, ~strike~,
// `code`, ```preformatted``` and "> " quotes — and they stay in Text, because
// that is what was sent and what an optimistic copy is matched against. This
// file draws them: the marks come out and the styles go in, at render time.
//
// The rules are Slack's own, as far as a reader can tell them apart: a mark
// opens only where a word starts and closes only where one ends, so
// snake_case_names and 2*3*4 stay as written, and nothing inside a URL is
// ever read as a mark — dropping an underscore from a link would open a
// different page.

type markStyle uint8

const (
	markBold markStyle = 1 << iota
	markItalic
	markStrike
	markCode
)

// run is a stretch of one line in one style.
type run struct {
	text  string
	style markStyle
}

// mrkdwnLine is one display line: its runs, and whether it is quoted.
type mrkdwnLine struct {
	runs  []run
	quote bool
	pre   bool
}

const codeFence = "```"

// parseMrkdwn splits text into display lines with the marks taken out. Empty
// lines are dropped, as they are for plain text.
func parseMrkdwn(text string) []mrkdwnLine {
	pieces := strings.Split(text, codeFence)
	if len(pieces)%2 == 0 {
		// An odd number of fences: the last one never closes, so it is text.
		last := len(pieces) - 1
		pieces[last-1] += codeFence + pieces[last]
		pieces = pieces[:last]
	}

	var (
		lines   []mrkdwnLine
		current mrkdwnLine
		started bool // current has had text put on it
	)
	flush := func() {
		if lineText(current) != "" {
			lines = append(lines, trimLine(current))
		}
		current, started = mrkdwnLine{}, false
	}
	for i, piece := range pieces {
		pre := i%2 == 1
		for j, part := range strings.Split(piece, "\n") {
			if j > 0 {
				flush()
			}
			if part == "" {
				continue
			}
			if pre {
				current.pre = !started || current.pre
				current.runs = append(current.runs, run{text: part, style: markCode})
			} else {
				if !started {
					if rest, ok := strings.CutPrefix(part, "> "); ok {
						current.quote, part = true, rest
					} else if part == ">" {
						current.quote, part = true, ""
					}
				}
				current.runs = append(current.runs, parseInline(part)...)
			}
			started = true
		}
	}
	flush()
	return lines
}

func lineText(line mrkdwnLine) string {
	var sb strings.Builder
	for _, r := range line.runs {
		sb.WriteString(r.text)
	}
	return strings.TrimSpace(sb.String())
}

// trimLine trims the line's ends, as plain lines are trimmed. A preformatted
// line keeps its indentation, which is part of what it says.
func trimLine(line mrkdwnLine) mrkdwnLine {
	runs := append([]run(nil), line.runs...)
	for len(runs) > 0 && !line.pre {
		runs[0].text = strings.TrimLeft(runs[0].text, " \t")
		if runs[0].text != "" {
			break
		}
		runs = runs[1:]
	}
	for len(runs) > 0 {
		last := len(runs) - 1
		runs[last].text = strings.TrimRight(runs[last].text, " \t\r")
		if runs[last].text != "" {
			break
		}
		runs = runs[:last]
	}
	line.runs = runs
	return line
}

// urlPattern finds the stretches no mark may open or close inside.
var urlPattern = regexp.MustCompile(`(?:https?|mailto):[^\s]+`)

// parseInline splits one line into runs.
func parseInline(line string) []run {
	rs := []rune(line)
	protected := make([]bool, len(rs))
	for _, loc := range urlPattern.FindAllStringIndex(line, -1) {
		start := len([]rune(line[:loc[0]]))
		end := start + len([]rune(line[loc[0]:loc[1]]))
		// "*https://x.com*" is a bold link, not a link ending in '*'.
		for start > 0 && end > start && isMark(rs[start-1]) && rs[end-1] == rs[start-1] {
			end--
		}
		for i := start; i < end; i++ {
			protected[i] = true
		}
	}
	// A fence that never closed is left as text, and so is any other row of
	// backticks: "``" is not an empty code span.
	for i := 0; i < len(rs); i++ {
		j := i
		for j < len(rs) && rs[j] == '`' {
			j++
		}
		for k := i; j-i > 1 && k < j; k++ {
			protected[k] = true
		}
		i = max(i, j-1)
	}
	var out []run
	parseRange(rs, 0, len(rs), 0, protected, &out)
	return mergeRuns(out)
}

func parseRange(rs []rune, start, end int, style markStyle, protected []bool, out *[]run) {
	plain := start
	emit := func(from, to int, s markStyle) {
		if to > from {
			*out = append(*out, run{text: string(rs[from:to]), style: s})
		}
	}
	for i := start; i < end; i++ {
		mark := rs[i]
		if !isMark(mark) || protected[i] || !opensAt(rs, i, end) {
			continue
		}
		closing := closesAt(rs, i, end, protected)
		if closing < 0 {
			continue
		}
		emit(plain, i, style)
		if mark == '`' {
			// Code is literal: nothing inside it is a mark.
			emit(i+1, closing, style|markCode)
		} else {
			parseRange(rs, i+1, closing, style|markOf(mark), protected, out)
		}
		plain = closing + 1
		i = closing
	}
	emit(plain, end, style)
}

func isMark(r rune) bool { return r == '*' || r == '_' || r == '~' || r == '`' }

func markOf(r rune) markStyle {
	switch r {
	case '*':
		return markBold
	case '_':
		return markItalic
	case '~':
		return markStrike
	}
	return markCode
}

// opensAt reports whether the mark at i can open: it starts a word and is
// followed by something other than space.
func opensAt(rs []rune, i, end int) bool {
	if i+1 >= end || unicode.IsSpace(rs[i+1]) {
		return false
	}
	return i == 0 || !isWordRune(rs[i-1])
}

// closesAt finds the mark that closes the one at i: the same mark, after
// something other than space, ending a word.
func closesAt(rs []rune, i, end int, protected []bool) int {
	mark := rs[i]
	for k := i + 2; k < end; k++ {
		if rs[k] != mark || protected[k] {
			continue
		}
		if mark == '`' {
			return k
		}
		if unicode.IsSpace(rs[k-1]) {
			continue
		}
		if k+1 < len(rs) && isWordRune(rs[k+1]) {
			continue
		}
		return k
	}
	return -1
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func mergeRuns(runs []run) []run {
	var out []run
	for _, r := range runs {
		if n := len(out); n > 0 && out[n-1].style == r.style {
			out[n-1].text += r.text
			continue
		}
		out = append(out, r)
	}
	return out
}

// renderMrkdwn draws text as display lines with styles in place of marks.
func renderMrkdwn(text string) []string {
	var lines []string
	for _, line := range parseMrkdwn(text) {
		var sb strings.Builder
		if line.quote {
			sb.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("│"))
			sb.WriteString(" ")
		}
		for _, r := range line.runs {
			sb.WriteString(styleRun(r))
		}
		lines = append(lines, sb.String())
	}
	return lines
}

// styleRun styles a run word by word. Wrapping breaks lines at spaces and
// each visual line is drawn on its own, so a style opened on one word and
// closed several words later would be lost on every line after the first.
func styleRun(r run) string {
	if r.style == 0 {
		return r.text
	}
	style := lipgloss.NewStyle()
	if r.style&markBold != 0 {
		style = style.Bold(true)
	}
	if r.style&markItalic != 0 {
		style = style.Italic(true)
	}
	if r.style&markStrike != 0 {
		style = style.Strikethrough(true)
	}
	if r.style&markCode != 0 {
		style = style.Foreground(ColorCode)
	}
	words := strings.Split(r.text, " ")
	for i, word := range words {
		if word != "" {
			words[i] = style.Render(word)
		}
	}
	return strings.Join(words, " ")
}

// plainMrkdwn is the text with its marks taken out, for the places that show
// text without styles: the chat list's preview line and /open's link list.
func plainMrkdwn(text string) string {
	lines := parseMrkdwn(text)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		plain := lineText(line)
		if line.quote {
			plain = "> " + plain
		}
		out = append(out, plain)
	}
	return strings.Join(out, "\n")
}

// displayText is a message's text as a reader sees it, marks and all taken
// out.
func displayText(msg models.Message) string {
	if msg.Format == models.FormatMrkdwn {
		return plainMrkdwn(msg.Text)
	}
	return msg.Text
}

// textLines splits text into the lines a message shows.
func textLines(text string, format models.TextFormat) []string {
	if format == models.FormatMrkdwn {
		return renderMrkdwn(text)
	}
	return splitNonEmptyLines(text)
}
