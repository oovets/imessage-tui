package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutTabStates(t *testing.T) {
	leaf := &LayoutNodeState{}
	tests := []struct {
		name       string
		state      LayoutState
		wantTabs   int
		wantActive int
		wantFirst  []string
	}{
		{
			name:      "written before tabs: one tab",
			state:     LayoutState{Root: leaf, LeafChatGUIDs: []string{"a"}},
			wantTabs:  1,
			wantFirst: []string{"a"},
		},
		{
			name: "tabs win over the legacy fields",
			state: LayoutState{
				Root: leaf, LeafChatGUIDs: []string{"b"},
				Tabs:      []TabLayoutState{{Root: leaf, LeafChatGUIDs: []string{"a"}}, {Root: leaf, LeafChatGUIDs: []string{"b"}}},
				ActiveTab: 1,
			},
			wantTabs: 2, wantActive: 1, wantFirst: []string{"a"},
		},
		{
			name: "active tab out of range",
			state: LayoutState{
				Tabs:      []TabLayoutState{{Root: leaf}},
				ActiveTab: 4,
			},
			wantTabs: 1,
		},
		{name: "empty", state: LayoutState{}},
	}
	for _, tt := range tests {
		tabs, active := tt.state.TabStates()
		if len(tabs) != tt.wantTabs || active != tt.wantActive {
			t.Errorf("%s: %d tabs, active %d; want %d, %d", tt.name, len(tabs), active, tt.wantTabs, tt.wantActive)
			continue
		}
		if tt.wantFirst != nil && (len(tabs[0].LeafChatGUIDs) != 1 || tabs[0].LeafChatGUIDs[0] != tt.wantFirst[0]) {
			t.Errorf("%s: first tab shows %v, want %v", tt.name, tabs[0].LeafChatGUIDs, tt.wantFirst)
		}
	}
}

// A layout file from before tabs still loads, as one tab.
func TestLoadLayoutStateWrittenBeforeTabs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "imessage-tui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"root":{"direction":1,"split_ratio":0.5,"left":{"direction":0,"split_ratio":0.5},"right":{"direction":0,"split_ratio":0.5}},"leaf_chat_guids":["a","b"],"focused_leaf_index":1}`
	if err := os.WriteFile(filepath.Join(dir, "layout_state.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	state, ok := LoadLayoutState()
	if !ok {
		t.Fatal("old layout file did not load")
	}
	tabs, active := state.TabStates()
	if len(tabs) != 1 || active != 0 || len(tabs[0].LeafChatGUIDs) != 2 || tabs[0].FocusedLeafIndex != 1 {
		t.Errorf("old layout loaded as %d tabs (active %d): %+v", len(tabs), active, tabs)
	}
}
