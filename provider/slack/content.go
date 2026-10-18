package slack

import (
	"strconv"
	"strings"

	"github.com/oovets/imessage-tui/emojiset"
	"github.com/oovets/imessage-tui/models"
	slackapi "github.com/slack-go/slack"
)

// Apps rarely put what they have to say in a message's text. An Alertmanager
// alert leaves the text empty and sends a legacy attachment — a title, a body,
// a colour bar. Linear sends a one-line summary as text and the issue itself
// in Block Kit. Reading the text alone showed the first as a blank row and the
// second without the issue it was about.
//
// So a message's body is assembled the way Slack's own client lays it out —
// the blocks when the app sent a layout, otherwise the text, then each
// attachment — and it is assembled as mrkdwn, so TextToPlain resolves the
// mentions, links, entities and emoji in all of it exactly as in typed text.

// card is one attachment's worth of mrkdwn, and the colour of its bar.
type card struct {
	text  string
	color string
}

// messageContent is everything a message shows, as display text, with the
// stretches that came from attachments marked so the renderer can draw their
// bars. Each part is converted on its own: TextToPlain changes lengths, so the
// offsets can only be taken once a part is in its final form.
func messageContent(msg *slackapi.Msg, userNames map[string]string) (string, []models.Accent) {
	text := strings.TrimSpace(TextToPlain(messageBody(msg), userNames))
	var accents []models.Accent
	for _, c := range messageCards(msg) {
		cardText := strings.TrimSpace(TextToPlain(c.text, userNames))
		if cardText == "" {
			continue
		}
		if text != "" {
			text += "\n"
		}
		accents = append(accents, models.Accent{Start: len(text), End: len(text) + len(cardText), Color: c.color})
		text += cardText
	}

	// A reply carries no visual link to its root in a flat message list, so
	// it gets a marker instead of a thread pane. When the reply is all
	// attachment, the marker joins the first bar rather than sitting on a
	// line of its own.
	if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
		text = replyMarker + text
		for i := range accents {
			accents[i].Start += len(replyMarker)
			accents[i].End += len(replyMarker)
		}
		if len(accents) > 0 && accents[0].Start == len(replyMarker) {
			accents[0].Start = 0
		}
	}
	return text, accents
}

const replyMarker = "↳ "

func messageCards(msg *slackapi.Msg) []card {
	var cards []card
	for i := range msg.Attachments {
		att := &msg.Attachments[i]
		if text := attachmentMrkdwn(att); text != "" {
			cards = append(cards, card{text: text, color: attachmentColor(att.Color)})
		}
	}
	return cards
}

// attachmentColor normalises the colour an app gave its attachment: Slack
// takes "good", "warning" and "danger" as well as hex, with or without '#'.
// Anything else gets the neutral bar.
func attachmentColor(color string) string {
	color = strings.ToLower(strings.TrimSpace(color))
	switch color {
	case "good":
		return "#2eb886"
	case "warning":
		return "#daa038"
	case "danger":
		return "#a30200"
	}
	hex := strings.TrimPrefix(color, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return ""
	}
	if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
		return ""
	}
	return "#" + hex
}

// messageBody chooses between the text and the blocks.
//
// Every message a person types carries a rich_text block that says what its
// text says, and the text is what TextToPlain was built for, so the text wins
// there. An app that sends a layout — sections, headers, context — means the
// layout: its text is only what the notification shows, and Slack's own
// client does not draw it.
func messageBody(msg *slackapi.Msg) string {
	blocks := msg.Blocks.BlockSet
	if hasLayout(blocks) || strings.TrimSpace(msg.Text) == "" {
		if body := blocksMrkdwn(blocks); body != "" {
			return body
		}
	}
	return msg.Text
}

func hasLayout(blocks []slackapi.Block) bool {
	for _, block := range blocks {
		if block.BlockType() != slackapi.MBTRichText {
			return true
		}
	}
	return false
}

// attachmentMrkdwn renders a legacy attachment top to bottom, as Slack stacks
// it. The fallback is the app's own plain summary, so it is used only when
// nothing else is there to show.
func attachmentMrkdwn(att *slackapi.Attachment) string {
	if att.FromURL != "" || att.OriginalURL != "" {
		// A link unfurl, or a message shared from another channel. The URL is
		// already in the text, and the pane fetches its own previews.
		return ""
	}
	parts := []string{att.Pretext, att.AuthorName, mrkdwnLink(att.TitleLink, att.Title), att.Text}
	for _, field := range att.Fields {
		parts = append(parts, fieldMrkdwn(field.Title, field.Value))
	}
	parts = append(parts, blocksMrkdwn(att.Blocks.BlockSet))
	if att.ImageURL != "" {
		parts = append(parts, mrkdwnLink(att.ImageURL, ""))
	}
	var buttons []string
	for _, action := range att.Actions {
		if action.URL != "" {
			buttons = append(buttons, mrkdwnLink(action.URL, action.Text))
		}
	}
	parts = append(parts, strings.Join(buttons, " · "), att.Footer)

	if body := joinNonEmpty(parts, "\n"); body != "" {
		return body
	}
	return att.Fallback
}

// fieldMrkdwn puts a field's label and value on one line when the value fits
// on one, the way a reader would write it down.
func fieldMrkdwn(title, value string) string {
	title, value = strings.TrimSpace(title), strings.TrimSpace(value)
	switch {
	case title == "":
		return value
	case value == "":
		return title
	case strings.Contains(value, "\n"):
		return title + "\n" + value
	default:
		return strings.TrimSuffix(title, ":") + ": " + value
	}
}

func blocksMrkdwn(blocks []slackapi.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, blockMrkdwn(block))
	}
	return joinNonEmpty(parts, "\n")
}

// blockMrkdwn renders the blocks that carry something to read. Inputs, menus
// and buttons without a URL do something only inside Slack, so they are
// left out rather than drawn as controls that cannot be used here.
func blockMrkdwn(block slackapi.Block) string {
	switch b := block.(type) {
	case *slackapi.SectionBlock:
		parts := []string{textObject(b.Text)}
		for _, field := range b.Fields {
			// Slack draws fields as a two-column grid of "*Label*\nvalue";
			// one line each reads better in a terminal.
			parts = append(parts, oneLineField(textObject(field)))
		}
		if b.Accessory != nil {
			parts = append(parts, buttonMrkdwn(b.Accessory.ButtonElement))
		}
		return joinNonEmpty(parts, "\n")
	case *slackapi.HeaderBlock:
		return textObject(b.Text)
	case *slackapi.ContextBlock:
		var parts []string
		for _, element := range b.ContextElements.Elements {
			if text, ok := element.(*slackapi.TextBlockObject); ok {
				parts = append(parts, textObject(text))
			}
		}
		return joinNonEmpty(parts, " · ")
	case *slackapi.RichTextBlock:
		return richTextMrkdwn(b.Elements)
	case *slackapi.MarkdownBlock:
		return escapeMrkdwn(b.Text)
	case *slackapi.ImageBlock:
		if b.ImageURL == "" {
			return ""
		}
		label := textObject(b.Title)
		if label == "" {
			label = escapeMrkdwn(b.AltText)
		}
		return mrkdwnLink(b.ImageURL, label)
	case *slackapi.VideoBlock:
		url := b.TitleURL
		if url == "" {
			url = b.VideoURL
		}
		return mrkdwnLink(url, textObject(b.Title))
	case *slackapi.ActionBlock:
		if b.Elements == nil {
			return ""
		}
		var buttons []string
		for _, element := range b.Elements.ElementSet {
			if button, ok := element.(*slackapi.ButtonBlockElement); ok {
				buttons = append(buttons, buttonMrkdwn(button))
			}
		}
		return joinNonEmpty(buttons, " · ")
	}
	return ""
}

// oneLineField joins "*Label*\nvalue" into "*Label* value". A field with more
// lines than that is a paragraph and keeps its breaks.
func oneLineField(field string) string {
	if strings.Count(field, "\n") == 1 {
		return strings.Replace(field, "\n", " ", 1)
	}
	return field
}

// buttonMrkdwn shows a button that opens a URL as a link to it.
func buttonMrkdwn(button *slackapi.ButtonBlockElement) string {
	if button == nil || button.URL == "" {
		return ""
	}
	return mrkdwnLink(button.URL, textObject(button.Text))
}

// textObject reads a Block Kit text object. mrkdwn arrives escaped already;
// plain_text is literal and is escaped so TextToPlain leaves it as written.
func textObject(text *slackapi.TextBlockObject) string {
	if text == nil {
		return ""
	}
	if text.Type == slackapi.PlainTextType {
		return escapeMrkdwn(text.Text)
	}
	return text.Text
}

// listBullets mark nesting the way Slack does, since the renderer trims the
// indentation that would otherwise show it.
var listBullets = []string{"•", "◦", "▪"}

// richTextMrkdwn turns rich_text — what Slack's composer sends — back into
// the mrkdwn the same message's text would hold.
func richTextMrkdwn(elements []slackapi.RichTextElement) string {
	var lines []string
	for _, element := range elements {
		switch e := element.(type) {
		case *slackapi.RichTextSection:
			lines = append(lines, richTextSectionMrkdwn(e.Elements))
		case *slackapi.RichTextList:
			for i, item := range e.Elements {
				section, ok := item.(*slackapi.RichTextSection)
				if !ok {
					continue
				}
				marker := listBullets[e.Indent%len(listBullets)]
				if e.Style == slackapi.RTEListOrdered {
					marker = strconv.Itoa(e.Offset+i+1) + "."
				}
				lines = append(lines, strings.Repeat("  ", e.Indent)+marker+" "+richTextSectionMrkdwn(section.Elements))
			}
		case *slackapi.RichTextQuote:
			for _, line := range strings.Split(richTextSectionMrkdwn(e.Elements), "\n") {
				lines = append(lines, "&gt; "+line)
			}
		case *slackapi.RichTextPreformatted:
			lines = append(lines, "```\n"+richTextSectionMrkdwn(e.Elements)+"\n```")
		}
	}
	return strings.Join(lines, "\n")
}

func richTextSectionMrkdwn(elements []slackapi.RichTextSectionElement) string {
	var sb strings.Builder
	for _, element := range elements {
		switch e := element.(type) {
		case *slackapi.RichTextSectionTextElement:
			sb.WriteString(styled(escapeMrkdwn(e.Text), e.Style))
		case *slackapi.RichTextSectionLinkElement:
			sb.WriteString(mrkdwnLink(e.URL, escapeMrkdwn(e.Text)))
		case *slackapi.RichTextSectionEmojiElement:
			sb.WriteString(emojiMrkdwn(e))
		case *slackapi.RichTextSectionUserElement:
			sb.WriteString("<@" + e.UserID + ">")
		case *slackapi.RichTextSectionChannelElement:
			sb.WriteString("<#" + e.ChannelID + ">")
		case *slackapi.RichTextSectionUserGroupElement:
			sb.WriteString("<!subteam^" + e.UsergroupID + ">")
		case *slackapi.RichTextSectionBroadcastElement:
			sb.WriteString("<!" + e.Range + ">")
		case *slackapi.RichTextSectionDateElement:
			if e.Fallback != nil && *e.Fallback != "" {
				sb.WriteString(escapeMrkdwn(*e.Fallback))
			} else {
				sb.WriteString(e.Timestamp.Time().Format("2006-01-02 15:04"))
			}
		case *slackapi.RichTextSectionColorElement:
			sb.WriteString(escapeMrkdwn(e.Value))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// emojiMrkdwn writes the shortcode when the shared table knows it, so typed
// and block-built text agree on every glyph, and Slack's own codepoints when
// it does not.
func emojiMrkdwn(e *slackapi.RichTextSectionEmojiElement) string {
	if _, ok := emojiset.Glyph(e.Name); ok {
		code := ":" + e.Name + ":"
		if e.SkinTone > 1 {
			code += ":skin-tone-" + strconv.Itoa(e.SkinTone) + ":"
		}
		return code
	}
	if glyph, ok := codepoints(e.Unicode); ok {
		return glyph
	}
	// A workspace-custom emoji: the name says more than nothing would.
	return ":" + e.Name + ":"
}

// codepoints decodes Slack's "1f44d-1f3fc" form.
func codepoints(unicode string) (string, bool) {
	if unicode == "" {
		return "", false
	}
	var sb strings.Builder
	for _, hex := range strings.Split(unicode, "-") {
		r, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return "", false
		}
		sb.WriteRune(rune(r))
	}
	return sb.String(), true
}

// styled puts mrkdwn's marks around styled rich text. The marks go inside any
// surrounding whitespace: "*bold *" is not bold in mrkdwn, and a reader would
// see a stray asterisk.
func styled(text string, style *slackapi.RichTextSectionTextStyle) string {
	core := strings.TrimSpace(text)
	if style == nil || core == "" {
		return text
	}
	start := strings.Index(text, core)
	lead, trail := text[:start], text[start+len(core):]
	if style.Code {
		core = "`" + core + "`"
	}
	if style.Strike {
		core = "~" + core + "~"
	}
	if style.Italic {
		core = "_" + core + "_"
	}
	if style.Bold {
		core = "*" + core + "*"
	}
	return lead + core + trail
}

// mrkdwnLink writes <url|label>, which TextToPlain shows as "label (url)".
// The URL is escaped like the rest of mrkdwn, and a '|' in it would end it
// early, so that one is percent-encoded.
func mrkdwnLink(url, label string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return label
	}
	url = strings.ReplaceAll(escapeMrkdwn(url), "|", "%7C")
	if strings.TrimSpace(label) == "" {
		return "<" + url + ">"
	}
	return "<" + url + "|" + label + ">"
}

// escapeMrkdwn is Slack's escaping, for literal text that is about to be read
// as mrkdwn. &amp; first, or the other two would be escaped twice.
func escapeMrkdwn(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	return strings.ReplaceAll(text, ">", "&gt;")
}

func joinNonEmpty(parts []string, sep string) string {
	kept := parts[:0:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}
