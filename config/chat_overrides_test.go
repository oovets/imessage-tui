package config

import (
	"testing"
)

func TestChatOverridesRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	state := ChatOverridesState{
		Aliases: map[string]string{
			"chat-a": "Family",
		},
	}
	if err := SaveChatOverrides(state); err != nil {
		t.Fatalf("SaveChatOverrides returned error: %v", err)
	}

	got := LoadChatOverrides()
	if got.Aliases["chat-a"] != "Family" {
		t.Fatalf("alias = %q, want Family", got.Aliases["chat-a"])
	}
}
