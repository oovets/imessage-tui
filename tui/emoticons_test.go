package tui

import "testing"

func TestEmoticonInlineConversion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{":)", "🙂"},
		{"hi :)", "hi 🙂"},
		{":D", "😄"},
		{":-)", "🙂"},
		{":*", "😘"},
		{";)", "😉"},
		{"i <3 you", "i ❤️ you"},
		{"xD", "😆"},
		{":'(", "😢"},
	}
	for _, tc := range cases {
		m := typeInput(tc.in)
		if got := m.GetText(); got != tc.want {
			t.Errorf("typeInput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEmoticonDoesNotMangleUrlsOrShortcodes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// ':/' inside a URL is preceded by a letter, not a boundary.
		{"http://x", "http://x"},
		// lowercase letter smileys are excluded so shortcodes keep working.
		{":pizza", ":pizza"},
		{":p", ":p"},
		// glued to a word -> no boundary -> left alone.
		{"lol:)", "lol:)"},
	}
	for _, tc := range cases {
		m := typeInput(tc.in)
		if got := m.GetText(); got != tc.want {
			t.Errorf("typeInput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A shortcode the popup handles must remain typeable without the emoticon pass
// eating it.
func TestEmoticonLeavesShortcodePopupIntact(t *testing.T) {
	m := typeInput(":piz")
	if got := m.GetText(); got != ":piz" {
		t.Fatalf("shortcode text mangled: %q", got)
	}
	if !m.AutocompleteActive() {
		t.Fatalf("expected shortcode popup to be active for ':piz'")
	}
}
