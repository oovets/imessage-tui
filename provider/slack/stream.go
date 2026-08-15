package slack

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Stream is a workspace's realtime feed over Socket Mode.
//
// Socket Mode needs the app-level token; a workspace configured with only a
// user token has no stream and falls back to the app's polling refresh.
type Stream struct {
	provider *Provider
	socket   *socketmode.Client

	events       chan provider.Event
	reconnected  chan struct{}
	disconnected chan struct{}
	overflowed   chan struct{}
}

var _ provider.Stream = (*Stream)(nil)

// NewStream returns a realtime feed for p, or nil when the workspace has no
// app-level token.
func NewStream(p *Provider) *Stream {
	if p == nil || strings.TrimSpace(p.ws.AppToken) == "" {
		return nil
	}
	return &Stream{
		provider: p,
		// The TUI owns the terminal, so socketmode's default logger — which
		// writes to stderr — would scribble over the rendered frame. Point it
		// at the app's log file like everything else.
		socket: socketmode.New(p.api, socketmode.OptionLog(log.Default())),
		// Deep enough to absorb a burst from a busy workspace while the UI is
		// blocked rendering; overflow is signalled rather than silently lost.
		events:       make(chan provider.Event, 256),
		reconnected:  make(chan struct{}, 4),
		disconnected: make(chan struct{}, 4),
		overflowed:   make(chan struct{}, 4),
	}
}

// Connect starts the Socket Mode loop.
//
// It returns without waiting for the connection: socketmode.Run reconnects on
// its own for the life of the process, so there is no single moment that
// "connected" means. The UI learns the state from Reconnected/Disconnected.
func (s *Stream) Connect() error {
	go func() {
		if err := s.socket.Run(); err != nil {
			log.Printf("[slack] %s: socket mode stopped: %v", s.provider.ID(), err)
			s.signal(s.disconnected)
		}
	}()
	go s.pump()
	return nil
}

func (s *Stream) Events() <-chan provider.Event { return s.events }
func (s *Stream) Reconnected() <-chan struct{}  { return s.reconnected }
func (s *Stream) Disconnected() <-chan struct{} { return s.disconnected }
func (s *Stream) Overflowed() <-chan struct{}   { return s.overflowed }

// pump translates socketmode events into provider events until the socket
// client closes its channel.
func (s *Stream) pump() {
	for event := range s.socket.Events {
		switch event.Type {
		case socketmode.EventTypeConnected:
			// Also fires on the first connection. The UI answers by resyncing
			// open chats, which is exactly right after a gap and merely
			// redundant at startup.
			s.signal(s.reconnected)

		case socketmode.EventTypeDisconnect, socketmode.EventTypeConnectionError:
			s.signal(s.disconnected)

		case socketmode.EventTypeInvalidAuth:
			log.Printf("[slack] %s: app token rejected, realtime is off", s.provider.ID())
			s.signal(s.disconnected)

		case socketmode.EventTypeEventsAPI:
			apiEvent, ok := event.Data.(slackevents.EventsAPIEvent)
			if !ok {
				continue
			}
			// Slack redelivers anything unacknowledged, so ack before doing
			// any work with it.
			if event.Request != nil {
				if err := s.socket.Ack(*event.Request); err != nil {
					log.Printf("[slack] ack failed: %v", err)
				}
			}
			s.handleEventsAPI(apiEvent, event.Request)
		}
	}
}

func (s *Stream) handleEventsAPI(event slackevents.EventsAPIEvent, request *socketmode.Request) {
	message, ok := event.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}

	switch message.SubType {
	case "message_deleted":
		// There is no "remove one message" path in the UI, and inventing one
		// for a single backend would mean a delete that iMessage can never
		// send. The periodic refresh replaces the pane's history wholesale, so
		// a deletion disappears within one poll interval.
		return

	case "message_changed":
		if message.Message == nil {
			return
		}
		s.publish(provider.EventUpdatedMessage, message.Channel, *message.Message, request)

	default:
		s.publish(provider.EventNewMessage, message.Channel, msgFromEvent(message), request)
	}
}

func (s *Stream) publish(kind provider.EventKind, channelID string, msg slackapi.Msg, request *socketmode.Request) {
	if channelID == "" || msg.Timestamp == "" {
		return
	}
	// slackevents drops the files array on the way in, so it is read back off
	// the raw envelope. Without this an image posted while the app is open
	// shows as an empty message until the next refresh.
	if len(msg.Files) == 0 {
		msg.Files = filesFromRequest(request)
	}

	chatGUID := ChatGUID(s.provider.ws.ID, channelID)
	names := s.provider.resolveSenders([]slackapi.Msg{msg})
	converted := s.provider.toMessage(chatGUID, channelID, &msg, names)
	s.provider.rememberLatest(channelID, []models.Message{converted})

	event := provider.Event{Kind: kind, ChatGUID: chatGUID, Message: converted}
	select {
	case s.events <- event:
	default:
		// Dropping the event and saying so beats blocking the socket reader:
		// the UI resyncs every open chat when it sees this.
		log.Printf("[slack] %s: event buffer full, dropped %s", s.provider.ID(), converted.GUID)
		s.signal(s.overflowed)
	}
}

// msgFromEvent rebuilds the message struct from a top-level message event.
// Slack puts the fields at the top level for a new message and inside a nested
// object for a change, and slackevents mirrors that split.
func msgFromEvent(event *slackevents.MessageEvent) slackapi.Msg {
	return slackapi.Msg{
		ClientMsgID:     event.ClientMsgID,
		Type:            event.Type,
		Channel:         event.Channel,
		User:            event.User,
		Text:            event.Text,
		Timestamp:       event.TimeStamp,
		ThreadTimestamp: event.ThreadTimeStamp,
		SubType:         event.SubType,
		BotID:           event.BotID,
		Username:        event.Username,
	}
}

// filesFromRequest digs the files array out of the raw Socket Mode envelope,
// looking in both places Slack puts a message body.
func filesFromRequest(request *socketmode.Request) []slackapi.File {
	if request == nil || len(request.Payload) == 0 {
		return nil
	}
	var payload struct {
		Event struct {
			Files   []slackapi.File `json:"files"`
			Message struct {
				Files []slackapi.File `json:"files"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		return nil
	}
	if len(payload.Event.Files) > 0 {
		return payload.Event.Files
	}
	return payload.Event.Message.Files
}

func (s *Stream) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
		// Already pending; a second one would only duplicate the resync.
	}
}
