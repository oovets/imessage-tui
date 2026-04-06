package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type UIState struct {
	ShowTimestamps  bool `json:"show_timestamps"`
	ShowLineNumbers bool `json:"show_line_numbers"`
	ShowChatList    bool `json:"show_chat_list"`
	ShowSenderNames bool `json:"show_sender_names"`
}

func uiStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "imessage-tui", "ui_state.json"), nil
}

func legacyUIStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bluebubbles-tui", "ui_state.json"), nil
}

func LoadUIState() UIState {
	defaults := UIState{
		ShowTimestamps:  true,
		ShowLineNumbers: true,
		ShowChatList:    true,
		ShowSenderNames: true,
	}
	path, err := uiStatePath()
	if err != nil {
		return defaults
	}
	data, err := os.ReadFile(path)
	if err != nil {
		legacyPath, legacyPathErr := legacyUIStatePath()
		if legacyPathErr != nil {
			return defaults
		}
		legacyData, legacyErr := os.ReadFile(legacyPath)
		if legacyErr != nil {
			return defaults
		}
		data = legacyData
	}
	// Start from defaults so newly added fields keep sane values
	// when older ui_state files don't contain them.
	s := defaults
	if err := json.Unmarshal(data, &s); err != nil {
		return defaults
	}
	return s
}

func SaveUIState(s UIState) error {
	path, err := uiStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
