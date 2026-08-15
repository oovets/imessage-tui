// Package provider is the seam between the UI and the chat backends behind it.
//
// The TUI knows conversations by GUID and nothing else. Which backend a GUID
// belongs to is *derived* from its prefix (see Registry) rather than stored on
// the chat, so the two can never drift and chats restored from the message
// cache need no migration. iMessage GUIDs arrive from BlueBubbles unprefixed
// and are the default route; every other backend adds a prefix.
package provider

import (
	"errors"
	"log"
	"strings"

	"github.com/oovets/imessage-tui/models"
)

// Provider is one chat backend. The interface holds only what every backend
// can do — anything optional is a separate interface below, so a backend that
// cannot rename a conversation simply does not implement ChatEditor instead of
// returning "unsupported" at runtime.
type Provider interface {
	// ID identifies this backend for logs and error messages, e.g. "imessage"
	// or "slack:acme".
	ID() string

	Chats(limit int) ([]models.Chat, error)
	Messages(chatGUID string, limit int) ([]models.Message, error)

	// Send delivers text to a chat. echoGUID is the GUID the UI already gave
	// its optimistic copy: a backend that can carry it through gets the echo
	// reconciled by GUID instead of by fingerprint. Backends that cannot may
	// ignore it.
	Send(chatGUID, text, replyToGUID, echoGUID string) error

	React(chatGUID, messageGUID, reaction string) error
	MarkRead(chatGUID string) error
}

// ChatEditor is implemented by backends whose conversations can be renamed or
// deleted from the client. iMessage can; Slack cannot.
type ChatEditor interface {
	DeleteChat(chatGUID string) error
	RenameChat(chatGUID, displayName string) error
}

// AttachmentStore is implemented by backends that can hand back the bytes of an
// attachment whose URL is not fetchable on its own — Slack's file URLs need
// the workspace token, and BlueBubbles attachments are addressed by GUID.
//
// The whole attachment is passed rather than its GUID: which field addresses
// the file is the backend's business, and a Slack attachment is reached by the
// URL it carries, not by an id the server would have to look up again.
type AttachmentStore interface {
	// DownloadAttachment returns the attachment's bytes and its MIME type.
	DownloadAttachment(att models.Attachment) ([]byte, string, error)
}

// LinkPreviewer is implemented by backends that can resolve a URL into preview
// metadata. iMessage routes this through the BlueBubbles server.
type LinkPreviewer interface {
	LinkPreview(rawURL string) (models.LinkPreview, error)
}

// EventKind is what happened to a message, normalized across backends.
type EventKind int

const (
	// EventUnknown is anything the UI has no reaction to. Providers may emit
	// it rather than filtering, so the stream stays a faithful record.
	EventUnknown EventKind = iota
	// EventNewMessage is a message that has not been seen before.
	EventNewMessage
	// EventUpdatedMessage is a change to an existing message: a reaction, an
	// edit. It must not mark a chat unread or reorder the list.
	EventUpdatedMessage
)

// Event is one realtime update. Providers translate their own wire format into
// this and keep the format to themselves — the alternative, having every
// backend imitate BlueBubbles' JSON, buys a smaller diff once and a lie
// afterwards.
type Event struct {
	Kind     EventKind
	ChatGUID string
	Message  models.Message
}

// Stream is a backend's realtime feed. All four channels stay open for the
// lifetime of the stream; a closed channel means the feed is finished.
//
// Reconnected and Overflowed exist because both are recoverable data loss: the
// UI resyncs every known chat rather than silently missing messages.
type Stream interface {
	// Connect opens the feed. It is called once, before the channels are read.
	Connect() error
	// Events carries message activity.
	Events() <-chan Event
	// Reconnected fires after the feed came back from a drop.
	Reconnected() <-chan struct{}
	// Disconnected fires when the feed drops.
	Disconnected() <-chan struct{}
	// Overflowed fires when the feed had to drop events to keep up.
	Overflowed() <-chan struct{}
}

// Registry maps a chat GUID to the backend that owns it.
//
// Routing is by prefix and nothing else. A backend registered with the empty
// prefix owns every GUID that matches no other prefix — that is iMessage,
// whose GUIDs come from BlueBubbles as they are.
type Registry struct {
	fallback Provider
	prefixes []prefixRoute
}

type prefixRoute struct {
	prefix   string
	provider Provider
}

// NewRegistry returns a registry whose unprefixed route is fallback. A nil
// fallback is allowed: it makes For return nil for unprefixed GUIDs, which is
// what the UI already handles for a backend that is not configured.
func NewRegistry(fallback Provider) *Registry {
	return &Registry{fallback: fallback}
}

// Register routes GUIDs starting with prefix to p. The prefix must be
// non-empty — the unprefixed route is the fallback passed to NewRegistry.
// Registering a nil provider, or an empty prefix, is a no-op.
func (r *Registry) Register(prefix string, p Provider) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || p == nil {
		return
	}
	r.prefixes = append(r.prefixes, prefixRoute{prefix: prefix, provider: p})
}

// For returns the backend owning chatGUID, or nil when none is configured.
func (r *Registry) For(chatGUID string) Provider {
	if r == nil {
		return nil
	}
	for _, route := range r.prefixes {
		if strings.HasPrefix(chatGUID, route.prefix) {
			return route.provider
		}
	}
	return r.fallback
}

// All returns every registered backend, fallback first. Used for the fan-out
// that loads the chat list.
func (r *Registry) All() []Provider {
	if r == nil {
		return nil
	}
	all := make([]Provider, 0, len(r.prefixes)+1)
	if r.fallback != nil {
		all = append(all, r.fallback)
	}
	for _, route := range r.prefixes {
		all = append(all, route.provider)
	}
	return all
}

// Merge fans several streams into one, so the UI reads a single feed no matter
// how many backends are connected. Nil streams are skipped; merging nothing
// returns nil, which the app treats as "no realtime".
func Merge(streams ...Stream) Stream {
	live := make([]Stream, 0, len(streams))
	for _, stream := range streams {
		if stream != nil {
			live = append(live, stream)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	default:
		return &merged{
			streams:      live,
			events:       make(chan Event, 64*len(live)),
			reconnected:  make(chan struct{}, 4*len(live)),
			disconnected: make(chan struct{}, 4*len(live)),
			overflowed:   make(chan struct{}, 4*len(live)),
		}
	}
}

type merged struct {
	streams      []Stream
	events       chan Event
	reconnected  chan struct{}
	disconnected chan struct{}
	overflowed   chan struct{}
}

// Connect starts every stream and fails only when they all do. One backend
// being unreachable must not cost the user realtime on the others.
func (m *merged) Connect() error {
	var (
		errs []error
		live int
	)
	for _, stream := range m.streams {
		if err := stream.Connect(); err != nil {
			errs = append(errs, err)
			continue
		}
		live++
		go forward(stream.Events(), m.events)
		go signal(stream.Reconnected(), m.reconnected)
		go signal(stream.Disconnected(), m.disconnected)
		go signal(stream.Overflowed(), m.overflowed)
	}
	if live == 0 && len(errs) > 0 {
		return errors.Join(errs...)
	}
	for _, err := range errs {
		log.Printf("[provider] realtime stream failed to connect: %v", err)
	}
	return nil
}

// forward copies events until the source closes. The merged channel is never
// closed: one backend finishing must not tell the UI the whole feed is over.
func forward(src <-chan Event, dst chan<- Event) {
	for event := range src {
		dst <- event
	}
}

func signal(src <-chan struct{}, dst chan<- struct{}) {
	for range src {
		select {
		case dst <- struct{}{}:
		default:
			// The UI reacts to these by resyncing every chat, so a second
			// signal while one is already queued would only duplicate work.
		}
	}
}

func (m *merged) Events() <-chan Event          { return m.events }
func (m *merged) Reconnected() <-chan struct{}  { return m.reconnected }
func (m *merged) Disconnected() <-chan struct{} { return m.disconnected }
func (m *merged) Overflowed() <-chan struct{}   { return m.overflowed }
