package irc

import (
	"log"

	"github.com/oovets/imessage-tui/provider"
)

// Stream is a network's realtime feed. The connection is the feed: the lines
// it reads are translated into provider events here, and its drops and
// returns become Disconnected and Reconnected.
type Stream struct {
	provider *Provider

	events       chan provider.Event
	reconnected  chan struct{}
	disconnected chan struct{}
	overflowed   chan struct{}
}

var _ provider.Stream = (*Stream)(nil)

func newStream(p *Provider) *Stream {
	return &Stream{
		provider: p,
		// Deep enough for a busy channel while the UI is busy; overflow is
		// signalled rather than silently lost.
		events:       make(chan provider.Event, 256),
		reconnected:  make(chan struct{}, 4),
		disconnected: make(chan struct{}, 4),
		overflowed:   make(chan struct{}, 4),
	}
}

// NewStream returns the network's realtime feed, or nil for a nil provider.
func NewStream(p *Provider) *Stream {
	if p == nil {
		return nil
	}
	return p.stream
}

// Connect is a no-op: the feed is the connection New opened, and lines that
// arrived before the UI got here are waiting in the buffer.
func (s *Stream) Connect() error { return nil }

func (s *Stream) Events() <-chan provider.Event { return s.events }
func (s *Stream) Reconnected() <-chan struct{}  { return s.reconnected }
func (s *Stream) Disconnected() <-chan struct{} { return s.disconnected }
func (s *Stream) Overflowed() <-chan struct{}   { return s.overflowed }

func (s *Stream) publish(event provider.Event) {
	if s == nil {
		return
	}
	select {
	case s.events <- event:
	default:
		// Blocking here would stall the connection's read loop, and with it
		// the PONGs that keep it alive. The UI resyncs on overflow.
		log.Printf("[irc] %s: event buffer full, dropped %s", s.provider.ID(), event.Message.GUID)
		s.signal(s.overflowed)
	}
}

func (s *Stream) signal(ch chan struct{}) {
	if s == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
		// Already pending; a second one would only duplicate the resync.
	}
}
