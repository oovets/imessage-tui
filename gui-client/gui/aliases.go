package gui

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/oovets/bluebubbles-gui/models"
)

type aliasStore struct {
	mu      sync.RWMutex
	aliases map[string]string
	path    string
}

var globalAliases = &aliasStore{aliases: make(map[string]string)}

// loadAliasStore reads aliases from disk into globalAliases.
func loadAliasStore() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".config", "bluebubbles-tui", "aliases.json")
	globalAliases.path = path

	data, err := os.ReadFile(path)
	if err != nil {
		return // doesn't exist yet — that's fine
	}
	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		log.Printf("[aliases] parse error: %v", err)
		return
	}
	globalAliases.mu.Lock()
	globalAliases.aliases = aliases
	globalAliases.mu.Unlock()
	log.Printf("[aliases] loaded %d aliases", len(aliases))
}

func (s *aliasStore) get(guid string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.aliases[guid]
}

func (s *aliasStore) set(guid, alias string) {
	s.mu.Lock()
	s.aliases[guid] = alias
	s.mu.Unlock()
	s.save()
}

func (s *aliasStore) delete(guid string) {
	s.mu.Lock()
	delete(s.aliases, guid)
	s.mu.Unlock()
	s.save()
}

func (s *aliasStore) save() {
	if s.path == "" {
		return
	}
	s.mu.RLock()
	data, err := json.MarshalIndent(s.aliases, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		log.Printf("[aliases] marshal error: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		log.Printf("[aliases] mkdir error: %v", err)
		return
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		log.Printf("[aliases] write error: %v", err)
	}
}

// chatDisplayName returns the alias if one is set, otherwise the chat's own display name.
func chatDisplayName(chat models.Chat) string {
	if alias := globalAliases.get(chat.GUID); alias != "" {
		return alias
	}
	return chat.GetDisplayName()
}
