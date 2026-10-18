package provider

import (
	"testing"

	"github.com/oovets/imessage-tui/models"
)

// stub is a provider that records nothing and answers nothing; the registry
// only ever compares identities.
type stub struct{ id string }

func (s stub) ID() string                                     { return s.id }
func (s stub) Chats(int) ([]models.Chat, error)               { return nil, nil }
func (s stub) Messages(string, int) ([]models.Message, error) { return nil, nil }
func (s stub) Send(string, string, string, string) error      { return nil }
func (s stub) React(string, string, string) error             { return nil }
func (s stub) MarkRead(string) error                          { return nil }

// Routing by GUID prefix is what lets a second backend exist at all: the chat
// list, the message cache and the persister all treat GUIDs as opaque, so the
// prefix is the only thing that says which backend owns a conversation.
func TestRegistryRoutesByGUIDPrefix(t *testing.T) {
	imessage := stub{id: "imessage"}
	slack := stub{id: "slack:acme"}

	reg := NewRegistry(imessage)
	reg.Register("sl:", slack)

	cases := map[string]string{
		"sl:acme:C123":                   "slack:acme",
		"sl:acme:C123:1700000000.000100": "slack:acme",
		"iMessage;-;+46701234567":        "imessage",
		"chat-a":                         "imessage",
		"":                               "imessage",
		// A prefix that only looks like Slack's must not be captured.
		"slack-but-not-really": "imessage",
	}
	for guid, want := range cases {
		got := reg.For(guid)
		if got == nil {
			t.Errorf("%q: no provider", guid)
			continue
		}
		if got.ID() != want {
			t.Errorf("%q routed to %s, want %s", guid, got.ID(), want)
		}
	}
}

// The app runs unconfigured in tests and before login, so a registry with no
// backend has to answer rather than panic.
func TestRegistryWithoutFallbackReturnsNil(t *testing.T) {
	reg := NewRegistry(nil)
	if got := reg.For("chat-a"); got != nil {
		t.Errorf("For returned %v, want nil", got)
	}
	if got := len(reg.All()); got != 0 {
		t.Errorf("All returned %d providers, want 0", got)
	}
}

func TestRegistryAllListsFallbackFirst(t *testing.T) {
	reg := NewRegistry(stub{id: "imessage"})
	reg.Register("sl:", stub{id: "slack:acme"})
	reg.Register("tg:", stub{id: "telegram"})

	var ids []string
	for _, p := range reg.All() {
		ids = append(ids, p.ID())
	}
	want := []string{"imessage", "slack:acme", "telegram"}
	if len(ids) != len(want) {
		t.Fatalf("All() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("All() = %v, want %v", ids, want)
		}
	}
}

// Registering a nil provider would make For return a non-nil interface holding
// a nil pointer, which every `!= nil` guard upstream would then wave through.
func TestRegistryIgnoresNilAndEmptyRegistrations(t *testing.T) {
	reg := NewRegistry(stub{id: "imessage"})
	reg.Register("sl:", nil)
	reg.Register("", stub{id: "ghost"})

	if got := reg.For("sl:acme:C1"); got == nil || got.ID() != "imessage" {
		t.Errorf("nil registration was routed to, got %v", got)
	}
	if got := len(reg.All()); got != 1 {
		t.Errorf("All() has %d providers, want 1", got)
	}
}

type labeled struct {
	stub
	label string
}

func (l labeled) Label() string { return l.label }

// The chat list groups by these labels, so a backend without a Labeler must
// still land somewhere stable, and the order must follow registration.
func TestRegistryLabelsFollowRegistrationOrder(t *testing.T) {
	reg := NewRegistry(labeled{stub{id: "imessage"}, "iMessage"})
	reg.Register("sl:acme:", labeled{stub{id: "slack:acme"}, "Slack · Acme"})
	reg.Register("x:", stub{id: "other"})

	cases := map[string]string{
		"chat-a":       "iMessage",
		"sl:acme:C123": "Slack · Acme",
		"x:1":          "other",
	}
	for guid, want := range cases {
		if got := reg.Label(guid); got != want {
			t.Errorf("Label(%q) = %q, want %q", guid, got, want)
		}
	}

	got := reg.Labels()
	want := []string{"iMessage", "Slack · Acme", "other"}
	if len(got) != len(want) {
		t.Fatalf("Labels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Labels() = %v, want %v", got, want)
		}
	}

	var unconfigured *Registry
	if got := unconfigured.Label("chat-a"); got != "" {
		t.Errorf("nil registry Label = %q, want empty", got)
	}
}
