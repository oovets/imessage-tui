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

// LayoutState is the saved pane layout.
//
// Root, LeafChatGUIDs and FocusedLeafIndex are the active tab, and were the
// whole layout before there were tabs. They are still written, mirroring the
// active entry of Tabs, so an older build reading the file keeps that tab
// instead of starting from nothing.
type LayoutState struct {
	Root             *LayoutNodeState `json:"root"`
	LeafChatGUIDs    []string         `json:"leaf_chat_guids"`
	FocusedLeafIndex int              `json:"focused_leaf_index"`

	Tabs      []TabLayoutState `json:"tabs,omitempty"`
	ActiveTab int              `json:"active_tab,omitempty"`
}

// TabLayoutState is one tab's panes: the split tree, the chat in each leaf in
// left-to-right order, and which leaf has focus.
type TabLayoutState struct {
	Name             string           `json:"name,omitempty"`
	Root             *LayoutNodeState `json:"root"`
	LeafChatGUIDs    []string         `json:"leaf_chat_guids"`
	FocusedLeafIndex int              `json:"focused_leaf_index"`
}

// TabStates returns the saved tabs and which one was on screen. A file
// written before tabs existed is one tab.
func (s LayoutState) TabStates() ([]TabLayoutState, int) {
	if len(s.Tabs) > 0 {
		active := s.ActiveTab
		if active < 0 || active >= len(s.Tabs) {
			active = 0
		}
		return s.Tabs, active
	}
	if s.Root == nil {
		return nil, 0
	}
	return []TabLayoutState{{
		Root:             s.Root,
		LeafChatGUIDs:    s.LeafChatGUIDs,
		FocusedLeafIndex: s.FocusedLeafIndex,
	}}, 0
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
	if tabs, _ := state.TabStates(); len(tabs) == 0 {
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
