package tui

import (
	"testing"
	"time"

	"github.com/oovets/imessage-tui/config"
)

func cached() config.CachedChatMessages {
	return config.CachedChatMessages{FetchedAtUnixMilli: 1}
}

// The per-chat fast path must merge into the cumulative state (not replace it),
// while a full saveMessages replaces everything. This guards against a
// regression where a single-chat update would drop every other chat.
func TestPersisterMessagesMergeAndReplace(t *testing.T) {
	p := newPersister()
	p.debounce = time.Hour // never auto-flush during the test
	t.Cleanup(func() {
		p.mu.Lock()
		if p.messagesTimer != nil {
			p.messagesTimer.Stop()
		}
		p.mu.Unlock()
	})

	p.saveMessagesChat("a", cached())
	p.saveMessagesChat("b", cached())

	p.mu.Lock()
	got := len(p.messages)
	_, hasA := p.messages["a"]
	_, hasB := p.messages["b"]
	dirty := p.messagesDirty
	p.mu.Unlock()
	if got != 2 || !hasA || !hasB {
		t.Fatalf("per-chat saves should accumulate; got %d chats (a=%v b=%v)", got, hasA, hasB)
	}
	if !dirty {
		t.Fatalf("a save should mark the state dirty")
	}

	// Updating one chat must not drop the others.
	p.saveMessagesChat("a", cached())
	p.mu.Lock()
	got = len(p.messages)
	p.mu.Unlock()
	if got != 2 {
		t.Fatalf("updating one chat should keep the rest; got %d", got)
	}

	// A full save replaces the whole map.
	p.saveMessages(config.MessageCacheState{Chats: map[string]config.CachedChatMessages{"c": cached()}})
	p.mu.Lock()
	got = len(p.messages)
	_, hasC := p.messages["c"]
	p.mu.Unlock()
	if got != 1 || !hasC {
		t.Fatalf("full save should replace the map; got %d chats (c=%v)", got, hasC)
	}
}

func TestPersisterFlushNoopWhenEmpty(t *testing.T) {
	p := newPersister()
	// Nothing saved yet -> flush must not panic and must stay not-dirty.
	p.flushMessages()
	p.mu.Lock()
	dirty := p.messagesDirty
	p.mu.Unlock()
	if dirty {
		t.Fatalf("flush with no saves should not be dirty")
	}
}
