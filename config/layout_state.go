package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type LayoutNodeState struct {
	Direction  int              `json:"direction"`
	SplitRatio float64          `json:"split_ratio"`
	Left       *LayoutNodeState `json:"left,omitempty"`
	Right      *LayoutNodeState `json:"right,omitempty"`
}

type LayoutState struct {
	Root             *LayoutNodeState `json:"root"`
	LeafChatGUIDs    []string         `json:"leaf_chat_guids"`
	FocusedLeafIndex int              `json:"focused_leaf_index"`
}

func layoutStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "imessage-tui", "layout_state.json"), nil
}

func legacyLayoutStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bluebubbles-tui", "layout_state.json"), nil
}

func LoadLayoutState() (LayoutState, bool) {
	path, err := layoutStatePath()
	if err != nil {
		return LayoutState{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		legacyPath, legacyPathErr := legacyLayoutStatePath()
		if legacyPathErr != nil {
			return LayoutState{}, false
		}
		legacyData, legacyErr := os.ReadFile(legacyPath)
		if legacyErr != nil {
			return LayoutState{}, false
		}
		data = legacyData
	}
	var state LayoutState
	if err := json.Unmarshal(data, &state); err != nil {
		return LayoutState{}, false
	}
	if state.Root == nil {
		return LayoutState{}, false
	}
	return state, true
}

func SaveLayoutState(state LayoutState) error {
	path, err := layoutStatePath()
	if err != nil {
		return err
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
