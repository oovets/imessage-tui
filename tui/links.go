package tui

import (
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/oovets/imessage-tui/models"
)

// linkSpan is a clickable stretch of one visual line, in display columns
// counted from the left edge of the message area. end is exclusive.
type linkSpan struct {
	start, end int
	url        string
}

// trimLinkToken strips what a URL picks up from the sentence around it: the
// brackets or quotes it was wrapped in and the punctuation after it, in any
// order. A closing parenthesis stays when the URL opened one itself, so
// https://en.wikipedia.org/wiki/Go_(game) survives while Slack's
// "label (https://x.com)" still loses its last character.
func trimLinkToken(field string) string {
	s := strings.TrimLeft(field, "<([{\"'")
	for s != "" {
		last := s[len(s)-1]
		switch {
		case strings.IndexByte(".,;:!?>]}\"'", last) >= 0:
			s = s[:len(s)-1]
		case last == ')' && strings.Count(s, ")") > strings.Count(s, "("):
			s = s[:len(s)-1]
		default:
			return s
		}
	}
	return s
}

// isOpenableLink accepts only http(s) URLs with a host. Message text is
// written by other people, and handing the system opener a file:// or
// custom-scheme URL from it would let a sender choose what runs here.
func isOpenableLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

// textLinks returns every openable URL in text, in order and exactly as
// written, so each one can be found again in the rendered line. A URL that
// appears twice is returned twice: both copies are drawn, both are clickable.
func textLinks(text string) []string {
	var links []string
	for _, field := range strings.Fields(text) {
		if raw := trimLinkToken(field); isOpenableLink(raw) {
			links = append(links, raw)
		}
	}
	return links
}

// messageLinks lists the links message msg shows, once each and in the order
// they appear on screen: those left in the text first, then the preview lines
// drawn below it. /open #N K counts in this order.
func messageLinks(msg models.Message) []string {
	previews := linkPreviewsForMessage(msg, 2)
	previewLinks := make([]string, 0, len(previews))
	for _, preview := range previews {
		previewLinks = append(previewLinks, preview.URL)
	}

	var links []string
	seen := make(map[string]struct{})
	add := func(link string) {
		if _, ok := seen[link]; ok || !isOpenableLink(link) {
			return
		}
		seen[link] = struct{}{}
		links = append(links, link)
	}
	for _, link := range textLinks(stripSupportedMediaLinks(msg.Text, previewLinks)) {
		add(link)
	}
	for _, link := range previewLinks {
		add(link)
	}
	return links
}

// wrappedLinkSpans locates links on the visual lines that one logical line
// wrapped into, returning the spans for each visual line. A URL longer than
// the pane is hard-broken across lines, so the lines are searched as one run
// of text with the continuation markers taken out, and a URL that straddles a
// break yields a span on every line it touches.
func wrappedLinkSpans(lines []string, links []string) [][]linkSpan {
	if len(links) == 0 {
		return nil
	}

	type segment struct {
		from int // byte offset of text within the joined run
		col  int // column text starts at on its visual line
		text string
	}
	segments := make([]segment, len(lines))
	var joined strings.Builder
	for i, line := range lines {
		plain := ansi.Strip(line)
		col := 0
		if i > 0 && strings.HasPrefix(plain, continuationMarker) {
			plain = plain[len(continuationMarker):]
			col = lipgloss.Width(continuationMarker)
		}
		segments[i] = segment{from: joined.Len(), col: col, text: plain}
		joined.WriteString(plain)
	}

	run := joined.String()
	spans := make([][]linkSpan, len(lines))
	cursor := 0
	for _, link := range links {
		at := strings.Index(run[cursor:], link)
		if at < 0 {
			continue
		}
		start := cursor + at
		end := start + len(link)
		cursor = end
		for i, seg := range segments {
			lo := max(start, seg.from)
			hi := min(end, seg.from+len(seg.text))
			if lo >= hi {
				continue
			}
			spans[i] = append(spans[i], linkSpan{
				start: seg.col + ansi.StringWidth(seg.text[:lo-seg.from]),
				end:   seg.col + ansi.StringWidth(seg.text[:hi-seg.from]),
				url:   link,
			})
		}
	}
	return spans
}

// shiftSpans moves spans right by offset columns, for a line drawn after
// leading padding.
func shiftSpans(spans []linkSpan, offset int) []linkSpan {
	if offset == 0 || len(spans) == 0 {
		return spans
	}
	shifted := make([]linkSpan, len(spans))
	for i, span := range spans {
		shifted[i] = linkSpan{start: span.start + offset, end: span.end + offset, url: span.url}
	}
	return shifted
}
