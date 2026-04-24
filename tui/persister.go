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

	pendingMessages *config.MessageCacheState
	messagesTimer   *time.Timer

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

func (p *persister) saveMessages(state config.MessageCacheState) {
	p.mu.Lock()
	p.pendingMessages = &state
	if p.messagesTimer == nil {
		p.messagesTimer = time.AfterFunc(p.debounce, p.flushMessages)
	} else {
		p.messagesTimer.Reset(p.debounce)
	}
	p.mu.Unlock()
}

func (p *persister) flushMessages() {
	p.mu.Lock()
	state := p.pendingMessages
	p.pendingMessages = nil
	p.mu.Unlock()
	if state == nil {
		return
	}
	if err := config.SaveMessageCache(*state); err != nil {
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
