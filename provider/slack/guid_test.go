package slack

import "testing"

func TestChatAndMessageGUIDRoundTrip(t *testing.T) {
	chat := ChatGUID("acme", "C123")
	if chat != "sl:acme:C123" {
		t.Fatalf("ChatGUID = %q", chat)
	}
	workspace, channel, ok := ParseChatGUID(chat)
	if !ok || workspace != "acme" || channel != "C123" {
		t.Fatalf("ParseChatGUID(%q) = %q, %q, %v", chat, workspace, channel, ok)
	}

	message := MessageGUID("acme", "C123", "1700000000.000100")
	if message != "sl:acme:C123:1700000000.000100" {
		t.Fatalf("MessageGUID = %q", message)
	}
	workspace, channel, ts, ok := ParseMessageGUID(message)
	if !ok || workspace != "acme" || channel != "C123" || ts != "1700000000.000100" {
		t.Fatalf("ParseMessageGUID(%q) = %q, %q, %q, %v", message, workspace, channel, ts, ok)
	}

	// A message GUID is also a valid chat GUID prefix, which is what lets the
	// registry route a reaction on a message to the right workspace.
	if workspace, channel, ok := ParseChatGUID(message); !ok || workspace != "acme" || channel != "C123" {
		t.Errorf("message guid did not parse as a chat guid: %q %q %v", workspace, channel, ok)
	}
}

func TestParseGUIDRejectsForeignGUIDs(t *testing.T) {
	foreign := []string{
		"iMessage;-;+46701234567",
		"chat-a",
		"",
		"sl:",
		"sl:acme",
		"slack:acme:C1", // a lookalike prefix must not parse
	}
	for _, guid := range foreign {
		if _, _, ok := ParseChatGUID(guid); ok {
			t.Errorf("ParseChatGUID(%q) accepted a non-slack guid", guid)
		}
	}
	// A chat GUID has no timestamp, so it is not a message GUID.
	if _, _, _, ok := ParseMessageGUID("sl:acme:C123"); ok {
		t.Error("ParseMessageGUID accepted a chat guid")
	}
}

func TestTSToMillis(t *testing.T) {
	tests := map[string]int64{
		"1700000000.000100": 1700000000000,
		"1700000000.500000": 1700000000500,
		"0":                 0,
		// Garbage must land on 0 rather than panicking; a message that silently
		// became a date in 1970 would sort to the top of the pane.
		"":         0,
		"not a ts": 0,
	}
	for ts, want := range tests {
		if got := TSToMillis(ts); got != want {
			t.Errorf("TSToMillis(%q) = %d, want %d", ts, got, want)
		}
	}
}

func TestPrettyGroupName(t *testing.T) {
	tests := map[string]string{
		"mpdm-anna--bob--carol-1": "anna, bob, carol",
		"mpdm-anna--bob-1":        "anna, bob",
		// Anything that is not Slack's internal shape is left alone.
		"team-standup": "team-standup",
		"":             "",
	}
	for in, want := range tests {
		if got := prettyGroupName(in); got != want {
			t.Errorf("prettyGroupName(%q) = %q, want %q", in, got, want)
		}
	}
}
