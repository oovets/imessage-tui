package slack

import (
	"encoding/json"
	"testing"

	slackapi "github.com/slack-go/slack"
)

// The fixtures are the shapes real apps send, with the hosts and names made
// up: an Alertmanager webhook, Linear, a script posting rich text, and what
// Slack's own composer adds to every typed message.
func TestMessageContent(t *testing.T) {
	names := map[string]string{"UANNA": "Anna"}

	tests := []struct {
		name string
		json string
		want string
	}{
		{
			// The case that showed as a blank row: everything is in the
			// attachment, and its body arrives HTML-escaped.
			name: "alert in a legacy attachment with empty text",
			json: `{"text": "", "attachments": [{
				"color": "a30200",
				"fallback": "[FIRING:1] TaskRestartLoop | <http://am.example:9093/#/alerts>",
				"title": ":red_circle: 1 firing · TaskRestartLoop",
				"title_link": "https://alerts.example.com/",
				"text": "• web-1: api restarted 6 times\n\nRun ` + "`nomad alloc logs &lt;alloc&gt; api`" + ` while it lives.",
				"mrkdwn_in": ["fallback", "pretext", "text"]}]}`,
			want: "🔴 1 firing · TaskRestartLoop (https://alerts.example.com/)\n" +
				"• web-1: api restarted 6 times\n\nRun `nomad alloc logs <alloc> api` while it lives.",
		},
		{
			name: "attachment with nothing but a fallback shows the fallback",
			json: `{"attachments": [{"fallback": "Build &lt;main&gt; failed: <https://ci.example.com/42|#42>"}]}`,
			want: "Build <main> failed: #42 (https://ci.example.com/42)",
		},
		{
			name: "attachment parts stack in Slack's order",
			json: `{"text": "deploy finished", "attachments": [{
				"pretext": "prod", "author_name": "ci-bot", "title": "web 1.4.2",
				"text": "all green", "footer": "CI", "image_url": "https://ci.example.com/graph.png",
				"fields": [
					{"title": "Duration:", "value": "3m", "short": true},
					{"title": "Changes", "value": "fix login\nbump deps", "short": false}
				],
				"actions": [
					{"type": "button", "text": "Open run", "url": "https://ci.example.com/run/7"},
					{"type": "button", "text": "Retry", "name": "retry", "value": "7"}
				]}]}`,
			want: "deploy finished\nprod\nci-bot\nweb 1.4.2\nall green\n" +
				"Duration: 3m\nChanges\nfix login\nbump deps\n" +
				"https://ci.example.com/graph.png\nOpen run (https://ci.example.com/run/7)\nCI",
		},
		{
			// Linear's text is the notification summary; the blocks are the
			// message, and the issue itself is in the attachment's blocks.
			name: "a layout replaces the notification text",
			json: `{"text": "Linear closed issue in Core team",
				"blocks": [{"type": "section", "text": {"type": "mrkdwn",
					"text": "*Linear* closed issue in <https://linear.example/team/CORE|Core> team"}}],
				"attachments": [{"fallback": "CORE-12 Login fails", "color": "#95a2b3", "blocks": [
					{"type": "section", "text": {"type": "mrkdwn",
						"text": "<https://linear.example/issue/CORE-12|CORE-12 Login fails | on Safari>"}},
					{"type": "context", "elements": [
						{"type": "image", "image_url": "https://linear.example/p.png", "alt_text": "priority"},
						{"type": "mrkdwn", "text": "*Priority*  Urgent"},
						{"type": "plain_text", "text": "Anna"}]}]}]}`,
			want: "*Linear* closed issue in Core (https://linear.example/team/CORE) team\n" +
				"CORE-12 Login fails | on Safari (https://linear.example/issue/CORE-12)\n" +
				"*Priority*  Urgent · Anna",
		},
		{
			// Slack's composer sends rich_text beside the text of every typed
			// message. Rendering both would show each message twice.
			name: "typed text wins over its own rich_text",
			json: `{"text": "hej <@UANNA> *nu*", "blocks": [{"type": "rich_text", "elements": [
				{"type": "rich_text_section", "elements": [
					{"type": "text", "text": "something else entirely"}]}]}]}`,
			want: "hej @Anna *nu*",
		},
		{
			name: "rich_text alone is rendered",
			json: `{"text": "", "blocks": [{"type": "rich_text", "elements": [
				{"type": "rich_text_section", "elements": [
					{"type": "emoji", "name": "satellite_antenna", "unicode": "1f4e1"},
					{"type": "text", "text": " "},
					{"type": "text", "text": "sweep ", "style": {"bold": true}},
					{"type": "text", "text": "needs <a> look & "},
					{"type": "user", "user_id": "UANNA"},
					{"type": "text", "text": "\n"}]},
				{"type": "rich_text_list", "style": "bullet", "indent": 0, "elements": [
					{"type": "rich_text_section", "elements": [
						{"type": "text", "text": "n952 upload", "style": {"code": true}}]}]},
				{"type": "rich_text_list", "style": "bullet", "indent": 1, "elements": [
					{"type": "rich_text_section", "elements": [
						{"type": "link", "url": "https://logs.example.com/?a=1&b=2", "text": "logs"}]}]},
				{"type": "rich_text_list", "style": "ordered", "offset": 2, "elements": [
					{"type": "rich_text_section", "elements": [{"type": "text", "text": "third"}]}]},
				{"type": "rich_text_quote", "elements": [{"type": "text", "text": "quoted"}]},
				{"type": "rich_text_preformatted", "elements": [{"type": "text", "text": "x := <-ch"}]}]}]}`,
			want: "📡 *sweep* needs <a> look & @Anna\n" +
				"• `n952 upload`\n" +
				"  ◦ logs (https://logs.example.com/?a=1&b=2)\n" +
				"3. third\n" +
				"> quoted\n" +
				"```\nx := <-ch\n```",
		},
		{
			name: "an emoji the table does not know uses Slack's codepoints",
			json: `{"blocks": [{"type": "rich_text", "elements": [{"type": "rich_text_section", "elements": [
				{"type": "emoji", "name": "not_in_any_table", "unicode": "1f44d-1f3fc"},
				{"type": "emoji", "name": "aspace-logo"}]}]}]}`,
			want: "👍\U0001F3FC:aspace-logo:",
		},
		{
			name: "header, fields, divider and buttons",
			json: `{"text": "Incident opened", "blocks": [
				{"type": "header", "text": {"type": "plain_text", "text": "Incident <42> & more"}},
				{"type": "section", "fields": [
					{"type": "mrkdwn", "text": "*Status:*\nFiring"},
					{"type": "mrkdwn", "text": "*Notes:*\nfirst\nsecond"}],
					"accessory": {"type": "button", "text": {"type": "plain_text", "text": "Ack"}, "action_id": "ack"}},
				{"type": "divider"},
				{"type": "actions", "elements": [
					{"type": "button", "text": {"type": "plain_text", "text": "Runbook"}, "url": "https://wiki.example.com/rb"},
					{"type": "button", "text": {"type": "plain_text", "text": "Resolve"}, "action_id": "resolve"}]},
				{"type": "image", "image_url": "https://graphs.example.com/cpu.png", "alt_text": "CPU"}]}`,
			want: "Incident <42> & more\n" +
				"*Status:* Firing\n*Notes:*\nfirst\nsecond\n" +
				"Runbook (https://wiki.example.com/rb)\n" +
				"CPU (https://graphs.example.com/cpu.png)",
		},
		{
			// A layout made only of controls has nothing to read, so the
			// text is all there is to show.
			name: "a layout with nothing readable falls back to the text",
			json: `{"text": "Approve the deploy?", "blocks": [{"type": "actions", "elements": [
				{"type": "button", "text": {"type": "plain_text", "text": "Approve"}, "action_id": "ok"}]}]}`,
			want: "Approve the deploy?",
		},
		{
			// Link previews are the pane's job; Slack's unfurl would repeat
			// the link's title under every message that has one.
			name: "link unfurls are left out",
			json: `{"text": "<https://news.example.com/a>", "attachments": [{
				"from_url": "https://news.example.com/a", "original_url": "https://news.example.com/a",
				"service_name": "News", "title": "Headline", "text": "Teaser"}]}`,
			want: "https://news.example.com/a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg slackapi.Msg
			if err := json.Unmarshal([]byte(tt.json), &msg); err != nil {
				t.Fatalf("fixture: %v", err)
			}
			if got := TextToPlain(messageMrkdwn(&msg), names); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestStyledKeepsMarksInsideWhitespace(t *testing.T) {
	bold := &slackapi.RichTextSectionTextStyle{Bold: true, Italic: true}
	if got := styled("  word ", bold); got != "  *_word_* " {
		t.Errorf("styled = %q", got)
	}
	if got := styled("   ", bold); got != "   " {
		t.Errorf("whitespace gained marks: %q", got)
	}
}

func TestMrkdwnLinkSurvivesAPipeInTheURL(t *testing.T) {
	got := TextToPlain(mrkdwnLink("https://x.example/a|b", "label"), nil)
	if got != "label (https://x.example/a%7Cb)" {
		t.Errorf("got %q", got)
	}
}
