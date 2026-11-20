package slack

import (
	"fmt"
	"testing"
	"time"
)

// fakeClock makes the provider's cache and both budgets read one clock that
// the test moves by hand, and returns the hand.
func fakeClock(p *Provider) func(time.Duration) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	read := func() time.Time { return now }
	p.now = read
	p.historyCalls.now = read
	p.replyCalls.now = read
	return func(d time.Duration) { now = now.Add(d) }
}

func TestCallBudget(t *testing.T) {
	b := newCallBudget(60, 3) // one a second, three at once
	now := time.Unix(0, 0)
	b.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !b.take() {
			t.Fatalf("call %d of the burst refused", i+1)
		}
	}
	if b.take() {
		t.Fatal("a fourth call went through a burst of three")
	}
	now = now.Add(time.Second)
	if !b.take() {
		t.Fatal("a second's refill did not allow a call")
	}
	// A load with nothing to fall back on goes into debt, which later
	// refreshes pay back.
	b.spend()
	b.spend()
	now = now.Add(time.Second)
	if b.take() {
		t.Fatal("the debt was not paid back before the next refresh")
	}
	now = now.Add(2 * time.Second)
	if !b.take() {
		t.Fatal("still refused once the debt was paid")
	}
}

// Sixteen Slack panes in one workspace, refreshed every ten seconds the way
// the UI's poll does: no minute may exceed what Slack allows, and every pane
// must still be brought up to date within a minute.
func TestSixteenPanesStayInsideTier3(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	var calls []string // channel per history call, in order
	fake.route("conversations.history", func(form map[string]string) any {
		calls = append(calls, form["channel"])
		return map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "UANNA", "text": "hej", "ts": "1700000100.000100"},
		}}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	advance := fakeClock(p)

	const panes, tick, minutes = 16, 10 * time.Second, 5
	type mark struct{ start, end int }
	var perMinute []mark
	for minute := 0; minute < minutes; minute++ {
		start := len(calls)
		for step := 0; step < int(time.Minute/tick); step++ {
			for i := 0; i < panes; i++ {
				if _, err := p.Messages(fmt.Sprintf("sl:acme:C%d", i), 50); err != nil {
					t.Fatalf("Messages: %v", err)
				}
			}
			advance(tick)
		}
		perMinute = append(perMinute, mark{start, len(calls)})
	}

	for minute, m := range perMinute {
		n := m.end - m.start
		limit := tier3PerMinute + tier3Burst
		if minute == 0 {
			limit += panes // the first load of each pane always goes
		}
		if n > limit {
			t.Errorf("minute %d made %d history calls, over %d", minute+1, n, limit)
		}
		if minute == 0 {
			continue
		}
		reached := map[string]bool{}
		for _, channel := range calls[m.start:m.end] {
			reached[channel] = true
		}
		if len(reached) != panes {
			t.Errorf("minute %d refreshed %d of %d panes; the rest went stale", minute+1, len(reached), panes)
		}
	}
	if steady := perMinute[minutes-1].end - perMinute[minutes-1].start; steady > 50 {
		t.Errorf("a steady minute made %d history calls; Slack allows about 50", steady)
	}
}

// The user's own send must not be answered from the cache: the optimistic
// copy times out, the pane refetches, and a stale history would hide the
// message just sent.
func TestASendBypassesTheCache(t *testing.T) {
	fake := newFakeSlack(t)
	fake.route("users.list", func(map[string]string) any {
		return map[string]any{"ok": true, "members": []any{}}
	})
	fake.route("conversations.history", func(map[string]string) any {
		return map[string]any{"ok": true, "messages": []any{}}
	})
	fake.route("chat.postMessage", func(map[string]string) any {
		return map[string]any{"ok": true, "channel": "C1", "ts": "1700000200.000100"}
	})
	p, err := newAgainst(fake, Workspace{ID: "acme", Name: "Acme", Token: "xoxp-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fakeClock(p)

	if _, err := p.Messages("sl:acme:C1", 50); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Messages("sl:acme:C1", 50); err != nil {
		t.Fatal(err)
	}
	if got := fake.callCount("conversations.history"); got != 1 {
		t.Fatalf("a load right after another asked Slack %d times, want the cache", got)
	}
	if err := p.Send("sl:acme:C1", "hej", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Messages("sl:acme:C1", 50); err != nil {
		t.Fatal(err)
	}
	if got := fake.callCount("conversations.history"); got != 2 {
		t.Fatalf("the load after a send was served from the cache (%d calls)", got)
	}
}
