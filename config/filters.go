package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Filter hides lines from view, the way WeeChat's /filter does: a line in a
// matching buffer, carrying matching tags, whose text matches the regex. The
// fields are kept as typed, and compiled by the UI; see tui/filters.go.
type Filter struct {
	Name string `json:"name"`
	// Buffers are WeeChat buffer names or masks: irc.libera.#go, irc.*,
	// !irc.libera.#test.
	Buffers []string `json:"buffers"`
	// Tags is the tag expression: * , + and ! as in WeeChat.
	Tags string `json:"tags"`
	// Regex is matched against the message, or "nick\tmessage" when it holds
	// a \t; * matches anything.
	Regex    string `json:"regex"`
	Disabled bool   `json:"disabled,omitempty"`
}

// Ignore drops a nick's messages from view for good, the way WeeChat's
// /ignore does. Server and channel narrow it; empty means everywhere.
type Ignore struct {
	// Mask is a nick, a mask with *, or a regex after "re:".
	Mask    string `json:"mask"`
	Server  string `json:"server,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// FiltersState is what filters.json holds.
type FiltersState struct {
	Filters []Filter `json:"filters,omitempty"`
	Ignores []Ignore `json:"ignores,omitempty"`
}

func filtersPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "imessage-tui", "filters.json"), nil
}

// LoadFilters reads the saved filters and ignores. A missing or unreadable
// file is no filters at all.
func LoadFilters() FiltersState {
	path, err := filtersPath()
	if err != nil {
		return FiltersState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FiltersState{}
	}
	var state FiltersState
	if err := json.Unmarshal(data, &state); err != nil {
		return FiltersState{}
	}
	return state
}

// SaveFilters writes the filters and ignores.
func SaveFilters(state FiltersState) error {
	path, err := filtersPath()
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
