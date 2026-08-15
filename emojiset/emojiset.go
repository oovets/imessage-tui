// Package emojiset is the one shortcode-to-emoji table the app works from.
//
// Two sources feed it. The generated Slack set (iamcal/emoji-data, the dataset
// Slack itself uses) covers everything an incoming message might carry —
// :saluting_face:, :melting_face:, and the rest of the modern additions.
// github.com/enescakir/emoji fills in whatever names it knows that the Slack
// set does not.
//
// One table for both directions matters: the composer's autocomplete and the
// renderer that decodes incoming text have to agree on what an emoji name is,
// or you can receive a glyph you cannot type, or type a name that renders as
// literal text at the other end.
package emojiset

import (
	"strings"
	"sync"

	"github.com/enescakir/emoji"
)

var (
	once   sync.Once
	merged map[string]string
)

// build merges the two sources. Slack wins on conflict: its names are what
// arrives on the wire, so they are the ones that must decode correctly.
func build() {
	merged = make(map[string]string, len(slackShortcodes)+2048)

	for code, glyph := range emoji.Map() {
		name := strings.Trim(code, ":")
		if name == "" {
			continue
		}
		merged[name] = glyph
	}
	for name, glyph := range slackShortcodes {
		merged[name] = glyph
	}
}

// Glyph returns the emoji for a shortcode name, given without its colons.
func Glyph(name string) (string, bool) {
	once.Do(build)
	glyph, ok := merged[name]
	return glyph, ok
}

// SkinTone returns the Unicode modifier for a "skin-tone-N" shortcode.
func SkinTone(name string) (string, bool) {
	tone, ok := skinTones[name]
	return tone, ok
}

// All returns every known name and its glyph. The map is rebuilt per call, so
// callers may keep or modify it; it is meant to be read once at startup.
func All() map[string]string {
	once.Do(build)
	out := make(map[string]string, len(merged))
	for name, glyph := range merged {
		out[name] = glyph
	}
	return out
}
