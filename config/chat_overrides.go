package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ChatOverridesState struct {
	Aliases map[string]string `json:"aliases"`
	// Pinned lists favourite chats by GUID, in the order the chat list's
	// Pinned section shows them.
	Pinned []string `json:"pinned,omitempty"`
	// Ignored lists chats by GUID that the chat list never shows.
	Ignored []string `json:"ignored,omitempty"`
	// SenderNames holds the chats whose sender names were toggled by hand
	// away from the default, by GUID: true shows them, false hides them.
	SenderNames map[string]bool `json:"sender_names,omitempty"`
}

func chatOverridesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "imessage-tui", "chat_overrides.json"), nil
}

func LoadChatOverrides() ChatOverridesState {
	defaults := ChatOverridesState{
		Aliases: make(map[string]string),
	}
	path, err := chatOverridesPath()
	if err != nil {
		return defaults
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	state := defaults
	if err := json.Unmarshal(data, &state); err != nil {
		return defaults
	}
	if state.Aliases == nil {
		state.Aliases = make(map[string]string)
	}
	return state
}

func SaveChatOverrides(state ChatOverridesState) error {
	path, err := chatOverridesPath()
	if err != nil {
		return err
	}
	if state.Aliases == nil {
		state.Aliases = make(map[string]string)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
