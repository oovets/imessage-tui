package slack

import (
	"regexp"
	"strings"

	"github.com/oovets/imessage-tui/emojiset"
)

// Slack does not send the text a human typed. Entities are wrapped in angle
// brackets — <https://x.com|the label>, <@U08846VMHT5>, <#C123|general> — and
// &, < and > are HTML-escaped.
//
// Rendering that raw shows the brackets and, worse, breaks link previews: the
// URL extractor reads "https://x.com|the label" as one token, so no preview is
// ever fetched.
//
// Ported from the desktop client's src/slack/mrkdwn.ts.
var (
	// <@U123> / <@U123|name> — the id is resolved against a name map.
	userMention = regexp.MustCompile(`<@([UVW][A-Z0-9]+)(?:\|([^>]*))?>`)
	// <#C123|general> — Slack all but always includes the name here.
	channelMention = regexp.MustCompile(`<#([CD][A-Z0-9]+)(?:\|([^>]*))?>`)
	// <!here>, <!channel>, <!subteam^S123|@team>.
	specialMention = regexp.MustCompile(`<!([^>|]+)(?:\|([^>]*))?>`)
	// Any remaining <url> or <url|label>.
	link = regexp.MustCompile(`<((?:https?|mailto):[^>|]+)(?:\|([^>]*))?>`)
	// :shortcode: — Slack stores emoji as names, not characters.
	shortcode = regexp.MustCompile(`:([a-zA-Z0-9_+-]+):`)
)

// TextToPlain turns Slack's mrkdwn into text the message renderer can show.
//
// userNames resolves <@U123> mentions that arrive without a label; an id with
// no known name is still more use as "@U123" than as a raw entity.
//
// Formatting marks (*bold*, _italic_, `code`) are deliberately left in place.
// The renderer treats message text as plain, and a Slack user reads *bold* as
// emphasis — stripping the markers would silently drop what someone wrote.
func TextToPlain(text string, userNames map[string]string) string {
	if text == "" {
		return ""
	}

	text = userMention.ReplaceAllStringFunc(text, func(match string) string {
		groups := userMention.FindStringSubmatch(match)
		id, label := groups[1], groups[2]
		if label != "" {
			return "@" + label
		}
		if name := userNames[id]; name != "" {
			return "@" + name
		}
		return "@" + id
	})

	text = channelMention.ReplaceAllStringFunc(text, func(match string) string {
		groups := channelMention.FindStringSubmatch(match)
		id, name := groups[1], groups[2]
		if name != "" {
			return "#" + name
		}
		return "#" + id
	})

	text = specialMention.ReplaceAllStringFunc(text, func(match string) string {
		groups := specialMention.FindStringSubmatch(match)
		kind, label := groups[1], groups[2]
		if label != "" {
			if strings.HasPrefix(label, "@") {
				return label
			}
			return "@" + label
		}
		// "subteam^S123" with no label carries nothing readable past the ^.
		return "@" + strings.SplitN(kind, "^", 2)[0]
	})

	// A bare <url> becomes the url; a labelled one keeps the label and appends
	// the target, so the link stays visible and previewable.
	text = link.ReplaceAllStringFunc(text, func(match string) string {
		groups := link.FindStringSubmatch(match)
		url, label := groups[1], groups[2]
		if label == "" || label == url {
			return url
		}
		return label + " (" + url + ")"
	})

	return replaceShortcodes(unescapeEntities(text))
}

// unescapeEntities reverses Slack's HTML escaping. Order matters: &amp; last,
// or "&amp;lt;" would decode twice and turn into "<".
func unescapeEntities(text string) string {
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	return strings.ReplaceAll(text, "&amp;", "&")
}

// replaceShortcodes turns :rotating_light: into 🚨 and composes skin tones,
// working from the shared table so the composer and the renderer agree on what
// an emoji name is.
//
// Not cosmetic: a shortcode renders as literal text in the message body, and
// the composer's own autocomplete works from the same emoji set, so leaving
// them would make sent and received text disagree about what an emoji is.
// Workspace-custom emoji (:aspace-logo:) have no Unicode character and are
// left exactly as they arrived.
func replaceShortcodes(text string) string {
	return shortcode.ReplaceAllStringFunc(text, func(match string) string {
		name := strings.Trim(match, ":")
		// A tone rides directly after the emoji it modifies, so it replaces
		// itself with the bare modifier rather than a glyph of its own.
		if tone, ok := emojiset.SkinTone(name); ok {
			return tone
		}
		if glyph, ok := emojiset.Glyph(name); ok {
			return glyph
		}
		// Workspace-custom emoji have no Unicode character. Leaving the name
		// as written says more than dropping it would.
		return match
	})
}
