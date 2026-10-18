package telegram

import (
	"context"
	"log"

	"github.com/gotd/td/tg"
	"github.com/oovets/imessage-tui/provider"
)

// Stream is an account's realtime feed: gotd's updates manager, with gap
// recovery, translated into provider events.
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
		// Deep enough for the burst of missed updates that arrives after a
		// reconnect; overflow is signalled rather than silently lost.
		events:       make(chan provider.Event, 256),
		reconnected:  make(chan struct{}, 4),
		disconnected: make(chan struct{}, 4),
		overflowed:   make(chan struct{}, 4),
	}
}

// NewStream returns the account's realtime feed, or nil for a provider with no
// connection.
func NewStream(p *Provider) *Stream {
	if p == nil {
		return nil
	}
	return p.stream
}

// Connect is a no-op: the feed is part of the connection New opened, and
// updates that arrived before the UI got here are waiting in the buffer.
func (s *Stream) Connect() error { return nil }

func (s *Stream) Events() <-chan provider.Event { return s.events }
func (s *Stream) Reconnected() <-chan struct{}  { return s.reconnected }
func (s *Stream) Disconnected() <-chan struct{} { return s.disconnected }
func (s *Stream) Overflowed() <-chan struct{}   { return s.overflowed }

func (s *Stream) publish(event provider.Event) {
	if s == nil {
		// A provider without a connection has no feed to publish to.
		return
	}
	select {
	case s.events <- event:
	default:
		// Dropping the event and saying so beats blocking the updates
		// manager: the UI resyncs every open chat when it sees this.
		log.Printf("[telegram] %s: event buffer full, dropped %s", s.provider.ID(), event.Message.GUID)
		s.signal(s.overflowed)
	}
}

func (s *Stream) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
		// Already pending; a second one would only duplicate the resync.
	}
}

// handle wires the update types the UI has a use for.
func (p *Provider) handle(d tg.UpdateDispatcher) {
	d.OnNewMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		p.onMessage(provider.EventNewMessage, e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		p.onMessage(provider.EventNewMessage, e, u.Message)
		return nil
	})
	d.OnEditMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		p.onMessage(provider.EventUpdatedMessage, e, u.Message)
		return nil
	})
	d.OnEditChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		p.onMessage(provider.EventUpdatedMessage, e, u.Message)
		return nil
	})
	d.OnMessageReactions(func(_ context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		// The update carries the counts and nothing else, and a pane ignores
		// an update for a message it already shows. So the chat's cached
		// history is dropped and the next refresh brings the new counts.
		p.peers.applyEntities(e)
		if peer, ok := peerOf(u.Peer); ok {
			p.forget(peer)
		}
		return nil
	})
	d.OnDeleteMessages(func(context.Context, tg.Entities, *tg.UpdateDeleteMessages) error {
		// Outside channels a deletion names message ids but not the chat,
		// so every cached history may now hold a deleted message. As with
		// Slack, the UI has no single-message removal; the next refresh
		// drops it.
		p.mu.Lock()
		p.history = make(map[Peer]cachedHistory)
		p.mu.Unlock()
		return nil
	})
	d.OnDeleteChannelMessages(func(_ context.Context, _ tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		p.forget(Peer{Kind: kindChannel, ID: u.ChannelID})
		return nil
	})
}

func (p *Provider) onMessage(kind provider.EventKind, e tg.Entities, raw tg.MessageClass) {
	p.peers.applyEntities(e)
	msg, ok := raw.(*tg.Message)
	if !ok {
		return
	}
	peer, ok := peerOf(msg.PeerID)
	if !ok {
		return
	}
	p.forget(peer)
	p.rememberLatest(peer, msg.ID)
	converted := p.toMessage(peer, msg)
	p.stream.publish(provider.Event{Kind: kind, ChatGUID: converted.ChatGUID, Message: converted})
}
