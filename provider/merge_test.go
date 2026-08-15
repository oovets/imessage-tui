package provider

import (
	"errors"
	"testing"
	"time"
)

type fakeStream struct {
	connectErr   error
	events       chan Event
	reconnected  chan struct{}
	disconnected chan struct{}
	overflowed   chan struct{}
}

func newFakeStream() *fakeStream {
	return &fakeStream{
		events:       make(chan Event, 4),
		reconnected:  make(chan struct{}, 4),
		disconnected: make(chan struct{}, 4),
		overflowed:   make(chan struct{}, 4),
	}
}

func (f *fakeStream) Connect() error                { return f.connectErr }
func (f *fakeStream) Events() <-chan Event          { return f.events }
func (f *fakeStream) Reconnected() <-chan struct{}  { return f.reconnected }
func (f *fakeStream) Disconnected() <-chan struct{} { return f.disconnected }
func (f *fakeStream) Overflowed() <-chan struct{}   { return f.overflowed }

func TestMergeFansSeveralFeedsIntoOne(t *testing.T) {
	first, second := newFakeStream(), newFakeStream()
	merged := Merge(first, second)
	if err := merged.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	first.events <- Event{Kind: EventNewMessage, ChatGUID: "chat-a"}
	second.events <- Event{Kind: EventNewMessage, ChatGUID: "sl:acme:C1"}

	seen := map[string]bool{}
	for range 2 {
		select {
		case event := <-merged.Events():
			seen[event.ChatGUID] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out, saw %v", seen)
		}
	}
	if !seen["chat-a"] || !seen["sl:acme:C1"] {
		t.Errorf("merged feed delivered %v, want both backends", seen)
	}
}

// One backend being unreachable must not cost the user realtime on the others.
func TestMergeSurvivesOneStreamFailingToConnect(t *testing.T) {
	broken := newFakeStream()
	broken.connectErr = errors.New("no route to host")
	working := newFakeStream()

	merged := Merge(broken, working)
	if err := merged.Connect(); err != nil {
		t.Fatalf("Connect returned %v, want nil while one stream still works", err)
	}

	working.events <- Event{Kind: EventNewMessage, ChatGUID: "chat-a"}
	select {
	case event := <-merged.Events():
		if event.ChatGUID != "chat-a" {
			t.Errorf("got %q", event.ChatGUID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("working stream never delivered")
	}
}

func TestMergeFailsOnlyWhenEveryStreamFails(t *testing.T) {
	first, second := newFakeStream(), newFakeStream()
	first.connectErr = errors.New("first down")
	second.connectErr = errors.New("second down")

	if err := Merge(first, second).Connect(); err == nil {
		t.Fatal("Connect succeeded with no working stream")
	}
}

// A single stream is passed through untouched: no goroutines, no extra buffer,
// and a closed feed still reaches the app as a closed channel.
func TestMergeReturnsTheOnlyStreamUnwrapped(t *testing.T) {
	only := newFakeStream()
	if got := Merge(only, nil); got != Stream(only) {
		t.Errorf("Merge wrapped a single stream: %T", got)
	}
	if got := Merge(); got != nil {
		t.Errorf("Merge() = %v, want nil", got)
	}
	if got := Merge(nil, nil); got != nil {
		t.Errorf("Merge(nil, nil) = %v, want nil", got)
	}
}

func TestMergeForwardsSignals(t *testing.T) {
	first, second := newFakeStream(), newFakeStream()
	merged := Merge(first, second)
	if err := merged.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	first.reconnected <- struct{}{}
	second.disconnected <- struct{}{}
	first.overflowed <- struct{}{}

	for name, ch := range map[string]<-chan struct{}{
		"reconnected":  merged.Reconnected(),
		"disconnected": merged.Disconnected(),
		"overflowed":   merged.Overflowed(),
	} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Errorf("%s never arrived", name)
		}
	}
}
