package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/oovets/bluebubbles-tui/models"
)

type CachedChatMessages struct {
	Messages           []models.Message `json:"messages"`
	FetchedAtUnixMilli int64            `json:"fetched_at_unix_milli"`
}

type MessageCacheState struct {
	Chats map[string]CachedChatMessages `json:"chats"`
}

func messageCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bluebubbles-tui", "message_cache.json"), nil
}

func LoadMessageCache() MessageCacheState {
	defaults := MessageCacheState{
		Chats: make(map[string]CachedChatMessages),
	}

	path, err := messageCachePath()
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
	if state.Chats == nil {
		state.Chats = make(map[string]CachedChatMessages)
	}
	return state
}

func SaveMessageCache(state MessageCacheState) error {
	path, err := messageCachePath()
	if err != nil {
		return err
	}
	if state.Chats == nil {
		state.Chats = make(map[string]CachedChatMessages)
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
