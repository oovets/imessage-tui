package slack

import "testing"

func TestTextToPlain(t *testing.T) {
	names := map[string]string{"U08846VMHT5": "Anna", "U999": "Bo"}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "labelled link keeps label and target",
			// The target has to survive: the link-preview extractor reads the
			// text, so dropping the url means no preview is ever fetched.
			in:   "see <https://example.com/x|the docs>",
			want: "see the docs (https://example.com/x)",
		},
		{
			name: "bare link unwraps",
			in:   "<https://example.com/x>",
			want: "https://example.com/x",
		},
		{
			name: "self-labelled link does not duplicate",
			in:   "<https://example.com|https://example.com>",
			want: "https://example.com",
		},
		{
			name: "user mention resolves against the name map",
			in:   "hej <@U08846VMHT5> hur går det",
			want: "hej @Anna hur går det",
		},
		{
			name: "user mention with inline label wins",
			in:   "<@U08846VMHT5|anna.b> ping",
			want: "@anna.b ping",
		},
		{
			name: "unknown user id is shown rather than dropped",
			in:   "<@UZZZZZZ> ping",
			want: "@UZZZZZZ ping",
		},
		{
			name: "channel mention",
			in:   "flytta till <#C123ABC|general>",
			want: "flytta till #general",
		},
		{
			name: "special mention",
			in:   "<!here> deploy nu",
			want: "@here deploy nu",
		},
		{
			name: "subteam without label loses the caret part",
			in:   "<!subteam^S123> kolla",
			want: "@subteam kolla",
		},
		{
			name: "subteam with label keeps one @",
			in:   "<!subteam^S123|@platform> kolla",
			want: "@platform kolla",
		},
		{
			name: "html entities are unescaped",
			in:   "a &lt; b &amp;&amp; c &gt; d",
			want: "a < b && c > d",
		},
		{
			name: "escaped entity does not decode twice",
			in:   "&amp;lt;",
			want: "&lt;",
		},
		{
			name: "shortcodes become emoji",
			in:   "läget :rotating_light: nu",
			want: "läget 🚨 nu",
		},
		{
			name: "skin tone composes onto the preceding emoji",
			in:   ":crossed_fingers::skin-tone-2:",
			want: "🤞\U0001F3FB",
		},
		{
			// Emoji 14 and later: the general-purpose library predates these,
			// so they arrived as literal text until the Slack set was added.
			name: "modern shortcodes resolve",
			in:   "bra jobbat :saluting_face: :melting_face:",
			want: "bra jobbat 🫡 🫠",
		},
		{
			name: "workspace-custom emoji is left alone",
			in:   "hej :aspace-logo: hej",
			want: "hej :aspace-logo: hej",
		},
		{
			name: "formatting marks are preserved",
			// The renderer shows plain text; stripping the markers would drop
			// emphasis the sender actually typed.
			in:   "*viktigt* och _kursivt_",
			want: "*viktigt* och _kursivt_",
		},
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
		{
			name: "several entities in one message",
			in:   "<@U999> se <https://x.se|här> i <#C1AB|drift> &amp; svara",
			want: "@Bo se här (https://x.se) i #drift & svara",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TextToPlain(tt.in, names); got != tt.want {
				t.Errorf("TextToPlain(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}
