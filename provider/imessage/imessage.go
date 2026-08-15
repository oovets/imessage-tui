// Package imessage adapts the BlueBubbles HTTP and WebSocket clients to the
// provider interfaces.
//
// Everything BlueBubbles-shaped stops here: the wire types, the event names,
// the JSON. The UI above sees only provider.Provider and provider.Event.
package imessage

import (
	"encoding/json"
	"log"

	"github.com/oovets/imessage-tui/api"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
	"github.com/oovets/imessage-tui/ws"
)

// Provider is the iMessage backend. It owns unprefixed GUIDs.
type Provider struct {
	api *api.Client
}

// Compile-time proof that the optional capabilities are actually wired up —
// they are reached by type assertion, so a signature drift would otherwise only
// surface as a silently missing feature at runtime.
var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.ChatEditor      = (*Provider)(nil)
	_ provider.AttachmentStore = (*Provider)(nil)
	_ provider.LinkPreviewer   = (*Provider)(nil)
)

// New returns a provider backed by client. A nil client yields a nil provider
// so callers can pass one straight through from an unconfigured setup.
func New(client *api.Client) *Provider {
	if client == nil {
		return nil
	}
	return &Provider{api: client}
}

func (p *Provider) ID() string { return "imessage" }

func (p *Provider) Chats(limit int) ([]models.Chat, error) {
	return p.api.GetChats(limit)
}

func (p *Provider) Messages(chatGUID string, limit int) ([]models.Message, error) {
	return p.api.GetMessages(chatGUID, limit)
}

// Send carries echoGUID through as the BlueBubbles tempGuid, which comes back
// on the confirming message so the optimistic copy is matched by GUID rather
// than by fingerprint.
func (p *Provider) Send(chatGUID, text, replyToGUID, echoGUID string) error {
	return p.api.SendMessageWithTempGUID(chatGUID, text, replyToGUID, echoGUID)
}

func (p *Provider) React(chatGUID, messageGUID, reaction string) error {
	return p.api.SendReaction(chatGUID, messageGUID, reaction, 0)
}

func (p *Provider) MarkRead(chatGUID string) error {
	return p.api.MarkChatRead(chatGUID)
}

func (p *Provider) DeleteChat(chatGUID string) error {
	return p.api.DeleteChat(chatGUID)
}

func (p *Provider) RenameChat(chatGUID, displayName string) error {
	return p.api.RenameChat(chatGUID, displayName)
}

func (p *Provider) DownloadAttachment(att models.Attachment) ([]byte, string, error) {
	return p.api.DownloadAttachment(att.GUID)
}

func (p *Provider) LinkPreview(rawURL string) (models.LinkPreview, error) {
	preview, err := p.api.GetLinkPreview(rawURL)
	if err != nil {
		return models.LinkPreview{}, err
	}
	return models.LinkPreview{
		URL:         rawURL,
		Title:       preview.Title,
		AuthorName:  preview.AuthorName,
		Description: preview.Description,
		SiteName:    preview.SiteName,
		ImageURL:    preview.ImageURL,
	}, nil
}

// Stream adapts the BlueBubbles websocket client to provider.Stream, turning
// its event envelopes into normalized events.
type Stream struct {
	ws     *ws.Client
	events chan provider.Event
}

var _ provider.Stream = (*Stream)(nil)

// NewStream returns a stream over client, or nil when client is nil.
func NewStream(client *ws.Client) *Stream {
	if client == nil {
		return nil
	}
	return &Stream{
		ws: client,
		// Same depth as the websocket client's own buffer: this goroutine only
		// unmarshals, so anything that backs up here is already backing up
		// there, and the client signals Overflow when it does.
		events: make(chan provider.Event, cap(client.Events)),
	}
}

func (s *Stream) Connect() error {
	if err := s.ws.Connect(); err != nil {
		return err
	}
	go s.translate()
	return nil
}

// translate converts BlueBubbles envelopes into provider events until the
// websocket client's channel closes.
func (s *Stream) translate() {
	defer close(s.events)
	for raw := range s.ws.Events {
		kind := eventKind(raw.Type)
		if kind == provider.EventUnknown {
			s.events <- provider.Event{Kind: kind}
			continue
		}
		msg, err := parseMessageEvent(raw)
		if err != nil {
			log.Printf("[imessage] %s unmarshal failed: %v", raw.Type, err)
			s.events <- provider.Event{Kind: provider.EventUnknown}
			continue
		}
		s.events <- provider.Event{Kind: kind, ChatGUID: msg.ChatGUID, Message: msg}
	}
}

func (s *Stream) Events() <-chan provider.Event { return s.events }
func (s *Stream) Reconnected() <-chan struct{}  { return s.ws.Reconnect }
func (s *Stream) Disconnected() <-chan struct{} { return s.ws.Disconnect }
func (s *Stream) Overflowed() <-chan struct{}   { return s.ws.Overflow }

func eventKind(wsType string) provider.EventKind {
	switch wsType {
	case "new-message":
		return provider.EventNewMessage
	case "updated-message":
		// Reactions/tapbacks and edited bodies both arrive as updated-message.
		return provider.EventUpdatedMessage
	default:
		return provider.EventUnknown
	}
}

// parseMessageEvent extracts a message plus its chat identity from a
// BlueBubbles event. new-message and updated-message share one shape: the
// message fields, with the chat arriving either as chatGuid or a chats array.
func parseMessageEvent(event models.WSEvent) (models.Message, error) {
	var wsMsg struct {
		models.Message
		ChatGUID string `json:"chatGuid"`
		Chats    []struct {
			GUID string `json:"guid"`
		} `json:"chats"`
	}
	if err := json.Unmarshal(event.Data, &wsMsg); err != nil {
		return models.Message{}, err
	}

	msg := wsMsg.Message
	if len(wsMsg.Chats) > 0 {
		msg.ChatGUID = wsMsg.Chats[0].GUID
	} else if msg.ChatGUID == "" {
		msg.ChatGUID = wsMsg.ChatGUID
	}
	return msg, nil
}
