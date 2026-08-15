package emojiset

import "testing"

// The names that arrive from Slack are the point of this table. Every one
// below rendered as literal text before the generated Slack set was added,
// because the general-purpose library predates them.
func TestModernSlackNamesResolve(t *testing.T) {
	want := map[string]string{
		"saluting_face":           "🫡",
		"melting_face":            "🫠",
		"face_holding_back_tears": "🥹",
		"heart_hands":             "🫶",
		"handshake":               "🤝",
		"rotating_light":          "🚨",
	}
	for name, glyph := range want {
		got, ok := Glyph(name)
		if !ok {
			t.Errorf("%q is not in the table", name)
			continue
		}
		if got != glyph {
			t.Errorf("%q = %q, want %q", name, got, glyph)
		}
	}
}

// Slack's own spellings have to win, since they are what comes over the wire.
func TestSlackSpellingsResolve(t *testing.T) {
	for _, name := range []string{"+1", "-1", "thumbsup", "joy", "heart", "bangbang", "question"} {
		if _, ok := Glyph(name); !ok {
			t.Errorf("%q is not in the table", name)
		}
	}
}

func TestUnknownNameIsNotInvented(t *testing.T) {
	// Workspace-custom emoji have no Unicode character, and guessing one would
	// be worse than showing the name.
	if glyph, ok := Glyph("aspace-logo"); ok {
		t.Errorf("custom emoji resolved to %q", glyph)
	}
}

func TestSkinTones(t *testing.T) {
	tone, ok := SkinTone("skin-tone-2")
	if !ok {
		t.Fatal("skin-tone-2 missing")
	}
	if tone != "\U0001F3FB" {
		t.Errorf("skin-tone-2 = %q", tone)
	}
	if _, ok := SkinTone("skin-tone-9"); ok {
		t.Error("skin-tone-9 resolved, but Slack only defines 2 through 6")
	}
}

// The table has to be big enough to be worth having, and All must not lose
// entries on the way out.
func TestAllCoversBothSources(t *testing.T) {
	all := All()
	if len(all) < len(slackShortcodes) {
		t.Errorf("All returned %d names, fewer than the %d Slack alone defines",
			len(all), len(slackShortcodes))
	}
	if got := all["saluting_face"]; got != "🫡" {
		t.Errorf("All is missing the Slack set: saluting_face = %q", got)
	}
	if _, ok := all["pizza"]; !ok {
		t.Error("All is missing the general-purpose set")
	}

	// A caller may keep the map; mutating it must not corrupt the table.
	all["saluting_face"] = "x"
	if glyph, _ := Glyph("saluting_face"); glyph != "🫡" {
		t.Errorf("mutating the returned map changed the table: %q", glyph)
	}
}
