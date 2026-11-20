// Package irc is the IRC backend, over ergochat/irc-go. Everything IRC-shaped
// stops here — the UI above sees only models.Chat, models.Message and
// provider.Event.
//
// One Provider is one network, registered under irc:<network id>:. It works
// against any server, and best behind a bouncer (soju, Ergo) that offers
// IRCv3 chathistory: IRC itself keeps no history. IRC.md has the design and
// its reasons.
package irc

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/oovets/imessage-tui/emojiset"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// callTimeout bounds every call the UI makes. The UI runs them as commands,
// and a call that never returns would leave a pane showing "loading" forever.
const callTimeout = 30 * time.Second

// historyTimeout bounds one CHATHISTORY request. A bouncer answers in well
// under a second; one that does not is treated as having no more to give.
const historyTimeout = 15 * time.Second

// settleDelay is how long after registration the chat list waits before it
// is read. A bouncer replays its JOINs right after registering, and a list
// read before they land would show none of the channels.
const settleDelay = 1500 * time.Millisecond

// targetsWindow is how far back CHATHISTORY TARGETS looks for conversations.
const targetsWindow = 30 * 24 * time.Hour

// outgoingTimeout is how long the pieces of a sent message wait for the rest
// of their echoes before being shown on their own. It matches the UI's own
// timeout for an optimistic copy.
const outgoingTimeout = 30 * time.Second

// Flood control: four lines at once, then one every two seconds. Servers
// disconnect a client that sends faster ("Excess Flood"), and a pasted
// paragraph is many lines.
const (
	sendBurst = 4
	sendEvery = 2 * time.Second
)

// requestedCaps are the IRCv3 capabilities this client can use. Each is
// optional; the provider checks what the server acknowledged before relying
// on one.
var requestedCaps = []string{
	"server-time",       // a message's time is when it was sent, not when it arrived
	"message-tags",      // msgid, replies and reactions
	"echo-message",      // the server confirms what was sent, with its msgid
	"batch",             // chathistory answers arrive in a batch
	"draft/chathistory", // history from the server
	"chathistory",
	"draft/read-marker", // read state shared with other clients
}

// tapbacks maps the UI's six reactions onto the emoji clients that show
// +draft/react draw.
var tapbacks = map[string]string{
	"love":      "❤️",
	"laugh":     "😂",
	"like":      "👍",
	"dislike":   "👎",
	"emphasize": "‼️",
	"question":  "❓",
}

// Network is one configured IRC network.
type Network struct {
	// ID is the slug in every GUID. It never changes once chats exist.
	ID string
	// Name is what the user calls the network, for labels.
	Name string
	// Server is host:port.
	Server string
	TLS    bool
	Nick   string
	// Login is the SASL account. With it, Password is the SASL password;
	// without it, Password is sent as the server password (PASS), which is
	// how a bouncer is often logged into.
	Login    string
	Password string
	// Channels are joined on every connect.
	Channels []string
}

// wire is what the provider needs from a connection: *ircevent.Connection in
// the program, a recorder in tests. Lines coming the other way arrive through
// the callbacks attach registers.
type wire interface {
	SendWithTags(tags map[string]string, command string, params ...string) error
	CurrentNick() string
	AcknowledgedCaps() map[string]string
	ISupport() map[string]string
	MaxMessageLength(target string) int
	Quit()
}

// session is what the current connection negotiated. It is read once per
// registration, not per line.
type session struct {
	caps       map[string]bool
	mapping    caseMapping
	chanTypes  string
	statusMsg  string
	historyMax int
}

func defaultSession() session {
	return session{caps: map[string]bool{}, mapping: mapRFC1459, chanTypes: "#&"}
}

func (s session) history() bool {
	return s.caps["batch"] && (s.caps["draft/chathistory"] || s.caps["chathistory"])
}

func (s session) isChannel(target string) bool {
	return target != "" && strings.ContainsRune(s.chanTypes, rune(target[0]))
}

// stripStatus removes a STATUSMSG prefix: "@#chan" is a message to the
// channel's operators, shown in the channel.
func (s session) stripStatus(target string) string {
	trimmed := strings.TrimLeft(target, s.statusMsg)
	if trimmed != target && s.isChannel(trimmed) {
		return trimmed
	}
	return target
}

// Provider is one IRC network.
type Provider struct {
	net     Network
	wire    wire
	stream  *Stream
	limiter *limiter
	settle  time.Duration

	// labelNetwork prefixes chat names with the network name.
	labelNetwork bool

	// ready closes on the first registration.
	ready     chan struct{}
	readyOnce sync.Once

	mu       sync.Mutex
	session  session
	logs     map[string]*chatLog // by folded target
	joined   map[string]bool     // folded channel names
	targets  bool                // CHATHISTORY TARGETS answered this connection
	waiters  map[string][]chan struct{}
	outgoing []*outgoing
	// reactions are the msgids of reactions already counted, so a reaction
	// seen live and again in history counts once.
	reactions map[string]struct{}
	// absorbed are the msgids of echoed pieces folded into one message (see
	// outgoing); history brings them back as lines of their own.
	absorbed     map[string]struct{}
	registeredAt time.Time
	connects     int
}

var (
	_ provider.Provider    = (*Provider)(nil)
	_ provider.Labeler     = (*Provider)(nil)
	_ provider.BufferNamer = (*Provider)(nil)
)

// newProvider is the one place a Provider is assembled, for New and tests.
func newProvider(net Network, w wire) *Provider {
	p := &Provider{
		net:       net,
		wire:      w,
		limiter:   &limiter{burst: sendBurst, every: sendEvery},
		settle:    settleDelay,
		ready:     make(chan struct{}),
		session:   defaultSession(),
		logs:      make(map[string]*chatLog),
		joined:    make(map[string]bool),
		waiters:   make(map[string][]chan struct{}),
		reactions: make(map[string]struct{}),
		absorbed:  make(map[string]struct{}),
	}
	p.stream = newStream(p)
	return p
}

// New connects to a network and returns once it is registered. The
// connection then lives as long as the provider, reconnecting by itself.
func New(net Network) (*Provider, error) {
	if strings.TrimSpace(net.Server) == "" {
		return nil, errors.New("no server")
	}
	if strings.TrimSpace(net.Nick) == "" {
		return nil, errors.New("no nick")
	}
	user := net.Login
	if user == "" {
		user = net.Nick
	}
	conn := &ircevent.Connection{
		Server:      net.Server,
		UseTLS:      net.TLS,
		Nick:        net.Nick,
		User:        user,
		RealName:    net.Nick,
		RequestCaps: append([]string(nil), requestedCaps...),
		Version:     "imessage-tui",
		Timeout:     callTimeout,
		KeepAlive:   4 * time.Minute,
		// The library's default is two minutes; a laptop waking up should
		// not wait that long for its chats.
		ReconnectFreq: 15 * time.Second,
		// The library logs to stdout unless told otherwise, and the TUI owns
		// stdout. log.Writer is the file main redirected the log to.
		Log: log.New(log.Writer(), "[irc "+net.ID+"] ", log.LstdFlags),
	}
	if net.Login != "" {
		conn.SASLLogin = net.Login
		conn.SASLPassword = net.Password
	} else if net.Password != "" {
		conn.Password = net.Password
	}

	p := newProvider(net, conn)
	p.attach(conn)
	if err := conn.Connect(); err != nil {
		return nil, err
	}
	go conn.Loop()
	return p, nil
}

// ID identifies this backend in logs and errors.
func (p *Provider) ID() string { return "irc:" + p.net.ID }

// Label is what the chat list calls this network.
func (p *Provider) Label() string {
	if p.labelNetwork && p.net.Name != "" {
		return "IRC · " + p.net.Name
	}
	return "IRC"
}

// GUIDPrefix is what this network's chats are registered under.
func (p *Provider) GUIDPrefix() string { return Prefix + p.net.ID + sep }

// ShowNetworkInNames labels this network's chats with its name — off with one
// network, where it is noise, and the only way to tell two #general apart
// with more.
func (p *Provider) ShowNetworkInNames(show bool) { p.labelNetwork = show }

// BufferName is the conversation's name as WeeChat writes it:
// irc.<network>.<target>, the target casefolded as in the GUID. /filter and
// /ignore match against it.
func (p *Provider) BufferName(chatGUID string) string {
	fold, err := p.targetOf(chatGUID)
	if err != nil {
		return ""
	}
	return "irc." + p.net.ID + "." + fold
}

// Close quits the server.
func (p *Provider) Close() { p.wire.Quit() }

// Chats lists the channels this client is in and the conversations the
// server has history for, most recently active first.
//
// IRC has no list of conversations: channels are what is joined right now,
// and private conversations exist only as history. So the list is put
// together from the configured channels, the JOINs the connection has seen,
// CHATHISTORY TARGETS when the server has it, and whatever has arrived since.
func (p *Provider) Chats(_ int) ([]models.Chat, error) {
	if err := p.waitReady(); err != nil {
		return nil, err
	}
	p.waitSettled()
	p.loadTargets()

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, name := range p.net.Channels {
		p.logFor(name, true)
	}
	configured := make(map[string]bool, len(p.net.Channels))
	for _, name := range p.net.Channels {
		configured[p.session.mapping.fold(name)] = true
	}

	chats := make([]models.Chat, 0, len(p.logs))
	for fold, l := range p.logs {
		if l.channel && !p.joined[fold] && !configured[fold] {
			// Parted or kicked: still in the logs for a pane that shows it,
			// but no longer something to pick from the list.
			continue
		}
		chat := models.Chat{
			GUID:           ChatGUID(p.net.ID, fold),
			DisplayName:    p.chatName(l.name),
			ChatIdentifier: l.name,
			UnreadCount:    l.unread(),
		}
		if p.labelNetwork {
			chat.ShortName = l.name
		}
		if last := l.last(); last != nil {
			chat.LastMessageText = last.Text
		}
		chat.LastMessageTime = l.lastActive()
		chats = append(chats, chat)
	}
	sort.SliceStable(chats, func(i, j int) bool {
		if chats[i].LastMessageTime != chats[j].LastMessageTime {
			return chats[i].LastMessageTime > chats[j].LastMessageTime
		}
		return chats[i].ChatIdentifier < chats[j].ChatIdentifier
	})
	return chats, nil
}

func (p *Provider) chatName(name string) string {
	if p.labelNetwork && p.net.Name != "" {
		return p.net.Name + " " + name
	}
	return name
}

// waitReady waits for the first registration.
func (p *Provider) waitReady() error {
	select {
	case <-p.ready:
		return nil
	case <-time.After(callTimeout):
		return fmt.Errorf("irc %s: not registered with the server", p.net.Name)
	}
}

// waitSettled lets the JOINs a bouncer replays after registration land
// before the list is read.
func (p *Provider) waitSettled() {
	p.mu.Lock()
	wait := time.Until(p.registeredAt.Add(p.settle))
	p.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// Messages returns a conversation's recent history, oldest first. It reads
// the local log, which the server's history fills once per connection and the
// live feed keeps current — the UI's refresh timer never reaches the server.
func (p *Provider) Messages(chatGUID string, limit int) ([]models.Message, error) {
	fold, err := p.targetOf(chatGUID)
	if err != nil {
		return nil, err
	}
	p.loadHistory(fold, limit)

	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.logs[fold]
	if l == nil {
		return []models.Message{}, nil
	}
	return l.tail(limit), nil
}

// loadHistory asks the server for a conversation's latest history, unless it
// already answered since the connection came up.
func (p *Provider) loadHistory(fold string, limit int) {
	p.mu.Lock()
	s := p.session
	l := p.logs[fold]
	if !s.history() || (l != nil && l.filled) {
		p.mu.Unlock()
		return
	}
	name := fold
	if l != nil {
		name = l.name
	}
	if limit <= 0 {
		limit = 50
	}
	if s.historyMax > 0 && limit > s.historyMax {
		limit = s.historyMax
	}
	key := "history " + fold
	done, first := p.addWaiter(key)
	p.mu.Unlock()

	if first {
		if err := p.wire.SendWithTags(nil, "CHATHISTORY", "LATEST", name, "*", strconv.Itoa(limit)); err != nil {
			log.Printf("[irc] %s: history for %s: %v", p.ID(), name, err)
			p.release(key)
			return
		}
	}
	select {
	case <-done:
	case <-time.After(historyTimeout):
		log.Printf("[irc] %s: history for %s timed out", p.ID(), name)
		p.mu.Lock()
		// Asking again on every refresh would stall each one for the full
		// timeout. The next reconnect asks again.
		p.logFor(name, s.isChannel(name)).filled = true
		p.mu.Unlock()
		p.release(key)
	}
}

// loadTargets asks the server which conversations have recent history, once
// per connection.
func (p *Provider) loadTargets() {
	p.mu.Lock()
	if !p.session.history() || p.targets {
		p.mu.Unlock()
		return
	}
	done, first := p.addWaiter("targets")
	p.mu.Unlock()

	if first {
		now := time.Now()
		if err := p.wire.SendWithTags(nil, "CHATHISTORY", "TARGETS",
			"timestamp="+wireTime(now.Add(time.Hour)),
			"timestamp="+wireTime(now.Add(-targetsWindow)),
			"100"); err != nil {
			p.release("targets")
			return
		}
	}
	select {
	case <-done:
	case <-time.After(historyTimeout):
		log.Printf("[irc] %s: history targets timed out", p.ID())
		p.mu.Lock()
		p.targets = true
		p.mu.Unlock()
		p.release("targets")
	}
}

// addWaiter registers interest in key's answer. first says no request for it
// is in flight yet, so the caller sends one. Called with p.mu held.
func (p *Provider) addWaiter(key string) (<-chan struct{}, bool) {
	ch := make(chan struct{})
	first := len(p.waiters[key]) == 0
	p.waiters[key] = append(p.waiters[key], ch)
	return ch, first
}

// release wakes everything waiting on key.
func (p *Provider) release(key string) {
	p.mu.Lock()
	waiting := p.waiters[key]
	delete(p.waiters, key)
	p.mu.Unlock()
	for _, ch := range waiting {
		close(ch)
	}
}

// Send delivers text to a conversation. IRC carries one line per message, so
// a multi-line message, or one longer than a line holds, goes as several. The
// echoes of those come back as one message again (see outgoing), so the pane
// shows what was typed rather than how it had to travel.
func (p *Provider) Send(chatGUID, text, replyToGUID, echoGUID string) error {
	fold, err := p.targetOf(chatGUID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	s := p.session
	name := p.logFor(fold, s.isChannel(fold)).name
	flushed := p.store(p.pruneOutgoing(time.Now()))
	p.mu.Unlock()
	p.publishAll(flushed)

	pieces := splitForWire(text, p.wire.MaxMessageLength(name))
	if len(pieces) == 0 {
		return nil
	}
	echo := s.caps["echo-message"]
	if echo && len(pieces) > 1 {
		p.mu.Lock()
		p.outgoing = append(p.outgoing, &outgoing{target: fold, pieces: pieces, text: text, at: time.Now()})
		p.mu.Unlock()
	}

	var tags map[string]string
	if _, target, msgid, ok := ParseMessageGUID(replyToGUID); ok && target == fold && !isSynthetic(msgid) && s.caps["message-tags"] {
		tags = map[string]string{"+draft/reply": msgid}
	}
	deadline := time.Now().Add(callTimeout)
	for i, piece := range pieces {
		if err := p.limiter.wait(deadline); err != nil {
			return fmt.Errorf("irc send: %w", err)
		}
		lineTags := tags
		if i > 0 {
			lineTags = nil
		}
		if err := p.wire.SendWithTags(lineTags, "PRIVMSG", name, piece); err != nil {
			return fmt.Errorf("irc send: %w", err)
		}
	}

	if !echo {
		// Without echo-message the server never says the message arrived,
		// so this is the only copy there will be. It carries the UI's own
		// GUID, which reconciles the optimistic copy by identity.
		now := time.Now().UnixMilli()
		id := syntheticID(now, s.mapping.fold(p.wire.CurrentNick()), text)
		guid := MessageGUID(p.net.ID, fold, id)
		if echoGUID != "" {
			guid = echoGUID
		}
		msg := models.Message{
			GUID:        guid,
			Text:        text,
			IsFromMe:    true,
			DateCreated: now,
			ChatGUID:    ChatGUID(p.net.ID, fold),
			ReplyToGUID: replyToGUID,
		}
		p.mu.Lock()
		added := p.store([]models.Message{msg})
		p.mu.Unlock()
		p.publishAll(added)
	}
	return nil
}

// React sends one of the UI's reactions as an IRCv3 +draft/react. It needs
// message-tags, and a message the server gave an id: that is what the
// reaction names.
func (p *Provider) React(chatGUID, messageGUID, reaction string) error {
	fold, err := p.targetOf(chatGUID)
	if err != nil {
		return err
	}
	_, target, msgid, ok := ParseMessageGUID(messageGUID)
	if !ok || target != fold {
		return fmt.Errorf("irc react: %q is not a message in this conversation", messageGUID)
	}
	if isSynthetic(msgid) {
		return errors.New("irc react: the server gave this message no id to react to")
	}
	p.mu.Lock()
	s := p.session
	name := p.logFor(fold, s.isChannel(fold)).name
	p.mu.Unlock()
	if !s.caps["message-tags"] {
		return fmt.Errorf("irc react: %s carries no reactions (it lacks message-tags)", p.net.Name)
	}

	emoji := reactionGlyph(reaction)
	if err := p.limiter.wait(time.Now().Add(callTimeout)); err != nil {
		return fmt.Errorf("irc react: %w", err)
	}
	tags := map[string]string{"+draft/react": emoji, "+draft/reply": msgid}
	if err := p.wire.SendWithTags(tags, "TAGMSG", name); err != nil {
		return fmt.Errorf("irc react: %w", err)
	}
	if !s.caps["echo-message"] {
		p.mu.Lock()
		p.applyReaction(fold, msgid, emoji, "")
		p.mu.Unlock()
	}
	return nil
}

func reactionGlyph(reaction string) string {
	if glyph, ok := tapbacks[reaction]; ok {
		return glyph
	}
	if glyph, ok := emojiset.Glyph(strings.Trim(reaction, ":")); ok {
		return glyph
	}
	return reaction
}

// MarkRead moves the read marker to the newest message. Behind a server with
// draft/read-marker the marker is shared with the user's other clients;
// otherwise it is this client's alone and lasts until it exits.
func (p *Provider) MarkRead(chatGUID string) error {
	fold, err := p.targetOf(chatGUID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	l := p.logs[fold]
	if l == nil || l.last() == nil {
		p.mu.Unlock()
		return nil
	}
	newest := l.last().DateCreated
	already := l.readKnown && l.lastRead >= newest
	l.lastRead, l.readKnown = newest, true
	name := l.name
	shared := p.session.caps["draft/read-marker"]
	p.mu.Unlock()

	if already || !shared {
		return nil
	}
	if err := p.wire.SendWithTags(nil, "MARKREAD", name, "timestamp="+wireTime(time.UnixMilli(newest))); err != nil {
		return fmt.Errorf("irc mark read: %w", err)
	}
	return nil
}

// targetOf is the folded target a chat GUID names on this network.
func (p *Provider) targetOf(chatGUID string) (string, error) {
	networkID, target, ok := ParseChatGUID(chatGUID)
	if !ok || networkID != p.net.ID {
		return "", fmt.Errorf("irc: %q is not a conversation on %s", chatGUID, p.net.Name)
	}
	return target, nil
}

// logFor returns the log for a target, creating it. name may be folded or
// not; the first unfolded spelling seen is the one kept. Called with p.mu held.
func (p *Provider) logFor(name string, channel bool) *chatLog {
	fold := p.session.mapping.fold(name)
	l := p.logs[fold]
	if l == nil {
		l = &chatLog{name: name, channel: channel}
		p.logs[fold] = l
	} else if l.name == fold && name != fold {
		l.name = name
	}
	return l
}

// wireTime is a timestamp as IRCv3 writes it.
func wireTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// limiter spaces out lines: burst at once, then one per every. It tracks the
// time the next line may go — a lone message leaves at once, and a paste
// drains at the rate the server tolerates.
type limiter struct {
	burst int
	every time.Duration

	mu   sync.Mutex
	next time.Time
}

// wait blocks until a line may be sent, or fails if that is past deadline.
func (l *limiter) wait(deadline time.Time) error {
	if l == nil || l.every <= 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	delay := l.next.Sub(now) - time.Duration(l.burst-1)*l.every
	if delay < 0 {
		delay = 0
	}
	if now.Add(delay).After(deadline) {
		l.mu.Unlock()
		return errors.New("too much queued to send; try again shortly")
	}
	l.next = l.next.Add(l.every)
	l.mu.Unlock()
	time.Sleep(delay)
	return nil
}
