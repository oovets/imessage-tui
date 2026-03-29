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
}

func uiStatePath() (string, error) {
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
	}
	path, err := uiStatePath()
	if err != nil {
		return defaults
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	var s UIState
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
