package tui

import (
	"log"
	"sync"
	"time"

	"github.com/oovets/imessage-tui/config"
)

// persister serialises and debounces all state writes to disk so the
// Bubble Tea Update loop never blocks on file I/O. Blocking the loop even
// for ~50ms causes dropped keystrokes when typing fast, because the
// terminal's input buffer overruns while we're not reading from it.
type persister struct {
	mu sync.Mutex

	// messages is the cumulative cache state, retained across flushes so a
	// single-chat update doesn't have to re-supply every other chat. Updates
	// always replace a whole entry (never mutate one in place), so flush can
	// marshal the slices off the lock without racing the Update loop.
	messages      map[string]config.CachedChatMessages
	messagesDirty bool
	messagesTimer *time.Timer

	pendingLayout *config.LayoutState
	layoutTimer   *time.Timer

	pendingUI *config.UIState
	uiTimer   *time.Timer

	// How long to wait for more updates before flushing. Short enough to
	// survive a crash, long enough to coalesce bursts.
	debounce time.Duration
}

func newPersister() *persister {
	return &persister{debounce: 250 * time.Millisecond}
}

// saveMessages replaces the entire cached message state.
func (p *persister) saveMessages(state config.MessageCacheState) {
	p.mu.Lock()
	p.messages = state.Chats
	p.markMessagesDirtyLocked()
	p.mu.Unlock()
}

// saveMessagesChat updates a single chat's cached messages, leaving the rest of
// the cumulative state untouched. This keeps the per-message hot path from
// copying every other chat on every incoming message. The caller must pass a
// slice it will not mutate afterwards.
func (p *persister) saveMessagesChat(chatGUID string, cached config.CachedChatMessages) {
	p.mu.Lock()
	if p.messages == nil {
		p.messages = make(map[string]config.CachedChatMessages)
	}
	p.messages[chatGUID] = cached
	p.markMessagesDirtyLocked()
	p.mu.Unlock()
}

// markMessagesDirtyLocked schedules a debounced flush. Caller must hold p.mu.
func (p *persister) markMessagesDirtyLocked() {
	p.messagesDirty = true
	if p.messagesTimer == nil {
		p.messagesTimer = time.AfterFunc(p.debounce, p.flushMessages)
	} else {
		p.messagesTimer.Reset(p.debounce)
	}
}

func (p *persister) flushMessages() {
	p.mu.Lock()
	if !p.messagesDirty || p.messages == nil {
		p.mu.Unlock()
		return
	}
	// Copy the map (slice headers shared, contents immutable) so the marshal
	// below runs off the lock without racing concurrent saves on the loop.
	snapshot := config.MessageCacheState{Chats: make(map[string]config.CachedChatMessages, len(p.messages))}
	for guid, cached := range p.messages {
		snapshot.Chats[guid] = cached
	}
	p.messagesDirty = false
	p.mu.Unlock()

	if err := config.SaveMessageCache(snapshot); err != nil {
		log.Printf("failed to save message cache: %v", err)
	}
}

func (p *persister) saveLayout(state config.LayoutState) {
	p.mu.Lock()
	p.pendingLayout = &state
	if p.layoutTimer == nil {
		p.layoutTimer = time.AfterFunc(p.debounce, p.flushLayout)
	} else {
		p.layoutTimer.Reset(p.debounce)
	}
	p.mu.Unlock()
}

func (p *persister) flushLayout() {
	p.mu.Lock()
	state := p.pendingLayout
	p.pendingLayout = nil
	p.mu.Unlock()
	if state == nil {
		return
	}
	if err := config.SaveLayoutState(*state); err != nil {
		log.Printf("failed to save layout state: %v", err)
	}
}

func (p *persister) saveUI(state config.UIState) {
	p.mu.Lock()
	p.pendingUI = &state
	if p.uiTimer == nil {
		p.uiTimer = time.AfterFunc(p.debounce, p.flushUI)
	} else {
		p.uiTimer.Reset(p.debounce)
	}
	p.mu.Unlock()
}

func (p *persister) flushUI() {
	p.mu.Lock()
	state := p.pendingUI
	p.pendingUI = nil
	p.mu.Unlock()
	if state == nil {
		return
	}
	if err := config.SaveUIState(*state); err != nil {
		log.Printf("failed to save ui state: %v", err)
	}
}

// flushAll is called on shutdown to ensure no pending writes are lost.
func (p *persister) flushAll() {
	p.mu.Lock()
	if p.messagesTimer != nil {
		p.messagesTimer.Stop()
	}
	if p.layoutTimer != nil {
		p.layoutTimer.Stop()
	}
	if p.uiTimer != nil {
		p.uiTimer.Stop()
	}
	p.mu.Unlock()

	p.flushMessages()
	p.flushLayout()
	p.flushUI()
}
