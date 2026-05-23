package models

import "testing"

func TestChatGetDisplayNamePrefersLocalDisplayName(t *testing.T) {
	chat := Chat{
		DisplayName:      "Server Name",
		LocalDisplayName: "Local Alias",
		Participants: []Handle{
			{DisplayName: "Alice"},
			{DisplayName: "Bob"},
		},
	}

	if got, want := chat.GetDisplayName(), "Local Alias"; got != want {
		t.Fatalf("GetDisplayName() = %q, want %q", got, want)
	}
}
