package slack

import (
	"sync"
	"time"
)

// Slack rates conversations.history and conversations.replies as Tier 3:
// about fifty calls a minute each, per workspace. The UI refreshes every open
// pane on a timer, so the number of calls grows with the number of panes —
// eight Slack panes refreshing every ten seconds already made 48 history
// calls a minute, and sixteen would make 96. These budgets keep each method
// under the limit with room to spare for opening a chat, whatever the
// number of panes or the poll interval.
const (
	tier3PerMinute = 40
	tier3Burst     = 10
)

// freshFor is the shortest time a load is served again without asking Slack
// at all. It matches the UI's own message-cache TTL; see shareInterval for
// how it grows with the number of chats being refreshed.
const freshFor = 20 * time.Second

// shareInterval is how long a chat waits between loads so that every chat
// being refreshed gets its turn: n chats sharing perMinute calls get one each
// every n/perMinute minutes. A plain token bucket is not enough on its own —
// the refresh asks for the same chats in the same order every time, so the
// first few would win every call and the rest would never be brought up to
// date. Never shorter than freshFor.
func shareInterval(active, perMinute int) time.Duration {
	interval := time.Duration(active) * time.Minute / time.Duration(perMinute)
	if interval < freshFor {
		return freshFor
	}
	return interval
}

// callBudget is a token bucket for one Slack method in one workspace.
//
// A refresh asks with take and, when the budget is spent, is served the last
// result instead — the pane is a little behind, and the realtime feed keeps
// new messages coming meanwhile. A load with nothing to fall back on, a chat
// opened for the first time, spends regardless: it would otherwise show an
// empty pane, and the debt it leaves is paid back by the refreshes after it.
type callBudget struct {
	perMinute float64
	burst     float64
	now       func() time.Time

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newCallBudget(perMinute, burst int) *callBudget {
	return &callBudget{
		perMinute: float64(perMinute),
		burst:     float64(burst),
		tokens:    float64(burst),
		now:       time.Now,
	}
}

// refill adds what has accrued since the last call. Called with b.mu held.
func (b *callBudget) refill() {
	now := b.now()
	if !b.last.IsZero() {
		b.tokens += now.Sub(b.last).Minutes() * b.perMinute
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
	}
	b.last = now
}

// take spends one call if one is left.
func (b *callBudget) take() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// spend spends one call whether or not one is left.
func (b *callBudget) spend() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	b.tokens--
}
