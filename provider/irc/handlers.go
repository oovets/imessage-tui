package irc

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// attach wires a connection's lines into the provider. Every callback runs on
// the connection's read loop, so none may block: a stalled loop stops
// answering PINGs and the server drops the connection.
func (p *Provider) attach(conn *ircevent.Connection) {
	conn.AddConnectCallback(func(ircmsg.Message) { p.onRegistered() })
	conn.AddDisconnectCallback(func(ircmsg.Message) { p.onDisconnected() })
	for _, command := range []string{"PRIVMSG", "NOTICE", "TAGMSG", "JOIN", "PART", "KICK", "QUIT", "NICK",
		ircevent.RPL_NAMREPLY, ircevent.RPL_ENDOFNAMES, "MARKREAD", "FAIL"} {
		conn.AddCallback(command, p.handle)
	}
	conn.AddBatchCallback(p.onBatch)
}

// onRegistered runs at the end of every registration, the first and each one
// after a reconnect.
func (p *Provider) onRegistered() {
	s := readSession(p.wire)

	p.mu.Lock()
	p.session = s
	p.connects++
	again := p.connects > 1
	p.registeredAt = time.Now()
	// The JOINs that follow say what this connection is in.
	p.joined = make(map[string]bool)
	if again {
		// Whatever was said while the connection was down is only in the
		// server's history; the next read of each conversation asks for it.
		for _, l := range p.logs {
			l.filled = false
		}
		p.targets = false
	}
	p.mu.Unlock()

	for _, line := range joinLines(p.net.Channels) {
		if err := p.wire.SendWithTags(nil, "JOIN", line); err != nil {
			log.Printf("[irc] %s: join: %v", p.ID(), err)
		}
	}
	p.readyOnce.Do(func() { close(p.ready) })
	if again {
		p.stream.signal(p.stream.reconnected)
	}
}

// onDisconnected runs when the connection drops, before any reconnect.
func (p *Provider) onDisconnected() {
	// Nothing in flight will be answered now; waiting out the timeout would
	// only hold up the UI.
	p.mu.Lock()
	keys := make([]string, 0, len(p.waiters))
	for key := range p.waiters {
		keys = append(keys, key)
	}
	p.mu.Unlock()
	for _, key := range keys {
		p.release(key)
	}
	p.stream.signal(p.stream.disconnected)
}

// readSession reads what a fresh registration negotiated.
func readSession(w wire) session {
	s := defaultSession()
	for name := range w.AcknowledgedCaps() {
		s.caps[name] = true
	}
	isupport := w.ISupport()
	s.mapping = parseCaseMapping(isupport["CASEMAPPING"])
	if types, ok := isupport["CHANTYPES"]; ok {
		s.chanTypes = types
	}
	s.statusMsg = isupport["STATUSMSG"]
	if prefix, ok := isupport["PREFIX"]; ok {
		// "(ov)@+": the modes, then the symbols NAMES puts before a nick.
		if _, symbols, found := strings.Cut(prefix, ")"); found {
			s.prefixes = symbols
		}
	}
	s.historyMax, _ = strconv.Atoi(isupport["CHATHISTORY"])
	return s
}

// joinLines packs channels into as few JOINs as fit on a line.
func joinLines(channels []string) []string {
	var (
		lines   []string
		current string
	)
	for _, channel := range channels {
		channel = strings.TrimSpace(channel)
		if channel == "" {
			continue
		}
		if current != "" && len(current)+1+len(channel) > 400 {
			lines = append(lines, current)
			current = ""
		}
		if current != "" {
			current += ","
		}
		current += channel
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// handle is one line from the live connection.
func (p *Provider) handle(line ircmsg.Message) {
	switch line.Command {
	case "PRIVMSG", "NOTICE":
		p.onMessage(line, true)
	case "TAGMSG":
		p.onTagmsg(line)
	case "JOIN", "PART", "KICK", "QUIT", "NICK", ircevent.RPL_NAMREPLY, ircevent.RPL_ENDOFNAMES:
		p.onMembership(line)
	case "MARKREAD":
		p.onMarkRead(line)
	case "FAIL":
		p.onFail(line)
	}
}

// onMessage stores a PRIVMSG or NOTICE, and publishes it when it is live
// rather than history.
func (p *Provider) onMessage(line ircmsg.Message, live bool) {
	p.mu.Lock()
	fold, msg, ok := p.toMessage(line)
	if !ok {
		p.mu.Unlock()
		return
	}
	var ready []models.Message
	held := false
	if live && msg.IsFromMe {
		ready, held = p.absorbEcho(fold, msg, time.Now())
	}
	if !held {
		ready = append(ready, msg)
	}
	stored := p.store(ready)
	p.mu.Unlock()
	if live {
		p.publishAll(stored)
	}
}

// store adds messages to their conversations' logs and returns the ones that
// were new. Called with p.mu held.
func (p *Provider) store(msgs []models.Message) []models.Message {
	var added []models.Message
	for _, msg := range msgs {
		if _, gone := p.absorbed[messageID(msg.GUID)]; gone {
			continue
		}
		_, fold, ok := ParseChatGUID(msg.ChatGUID)
		if !ok {
			continue
		}
		l := p.logs[fold]
		if l == nil {
			continue
		}
		if l.add(msg) {
			added = append(added, msg)
		}
	}
	return added
}

// messageID is the msgid part of a message GUID.
func messageID(guid string) string {
	_, _, msgid, _ := ParseMessageGUID(guid)
	return msgid
}

func (p *Provider) publishAll(msgs []models.Message) {
	for _, msg := range msgs {
		p.stream.publish(provider.Event{Kind: provider.EventNewMessage, ChatGUID: msg.ChatGUID, Message: msg})
	}
}

// toMessage converts a PRIVMSG or NOTICE and makes sure its conversation has
// a log. ok is false for lines no one reads: server notices, CTCP requests,
// empty text. Called with p.mu held.
func (p *Provider) toMessage(line ircmsg.Message) (string, models.Message, bool) {
	if len(line.Params) < 2 {
		return "", models.Message{}, false
	}
	nick, chat, channel, ok := p.conversationOf(line)
	if !ok {
		return "", models.Message{}, false
	}
	s := p.session
	raw := line.Params[len(line.Params)-1]

	var (
		text   string
		format models.TextFormat
	)
	if command, arg, isCTCP := parseCTCP(raw); isCTCP {
		if command != "ACTION" || line.Command != "PRIVMSG" {
			return "", models.Message{}, false
		}
		body, f := convertText(arg)
		text, format = "* "+nick+" "+body, f
	} else {
		text, format = convertText(raw)
	}
	if strings.TrimSpace(text) == "" {
		return "", models.Message{}, false
	}

	when := lineTime(line)
	msgid := ""
	if present, value := line.GetTag("msgid"); present && value != "" {
		msgid = value
	} else {
		msgid = syntheticID(when, nick, raw)
	}
	fold := s.mapping.fold(chat)
	p.logFor(chat, channel)
	msg := models.Message{
		GUID:        MessageGUID(p.net.ID, fold, msgid),
		Text:        text,
		Format:      format,
		IsFromMe:    p.isMe(nick),
		DateCreated: when,
		ChatGUID:    ChatGUID(p.net.ID, fold),
	}
	if !msg.IsFromMe {
		msg.Handle = &models.Handle{Address: nick, DisplayName: nick}
	}
	if present, reply := line.GetTag("+draft/reply"); present && reply != "" {
		msg.ReplyToGUID = MessageGUID(p.net.ID, fold, reply)
	}
	return fold, msg, true
}

// conversationOf works out who sent a line and which conversation it belongs
// to: the channel, or for a private message the other person — the sender
// when it came in, the recipient when it is the user's own echo. ok is false
// for lines from the server itself. Called with p.mu held.
func (p *Provider) conversationOf(line ircmsg.Message) (nick, chat string, channel, ok bool) {
	if len(line.Params) < 1 || line.Source == "" {
		return "", "", false, false
	}
	nick = line.Nick()
	if !strings.Contains(line.Source, "!") && strings.Contains(line.Source, ".") {
		// A server name, not a person: connection notices, MOTD-ish chatter.
		return "", "", false, false
	}
	s := p.session
	target := s.stripStatus(line.Params[0])
	switch {
	case s.isChannel(target):
		return nick, target, true, true
	case p.isMe(nick):
		return nick, target, false, target != "" && target != "*"
	default:
		return nick, nick, false, true
	}
}

// isMe reports whether nick is the user's. Called with p.mu held.
func (p *Provider) isMe(nick string) bool {
	me := p.wire.CurrentNick()
	return me != "" && p.session.mapping.fold(nick) == p.session.mapping.fold(me)
}

// lineTime is when a line was sent: its server-time tag, or now.
func lineTime(line ircmsg.Message) int64 {
	if present, value := line.GetTag("time"); present {
		if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return t.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}

// onTagmsg handles a TAGMSG, which carries a reaction or nothing a pane
// shows. Live or from history, it is counted onto the message it names.
//
// No event is published: a pane ignores an update for a message it already
// shows, so the new count reaches it on the UI's next refresh, which reads
// the log — the same route Telegram's reactions take.
func (p *Provider) onTagmsg(line ircmsg.Message) {
	present, emoji := line.GetTag("+draft/react")
	if !present || emoji == "" {
		return
	}
	present, target := line.GetTag("+draft/reply")
	if !present || target == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, chat, _, ok := p.conversationOf(line)
	if !ok {
		return
	}
	_, reactionID := line.GetTag("msgid")
	p.applyReaction(p.session.mapping.fold(chat), target, emoji, reactionID)
}

// applyReaction counts emoji onto the message msgid in a conversation.
// reactionID, when the server gave one, makes counting the same reaction
// twice a no-op. Called with p.mu held.
func (p *Provider) applyReaction(fold, msgid, emoji, reactionID string) {
	if reactionID != "" {
		if _, seen := p.reactions[reactionID]; seen {
			return
		}
	}
	l := p.logs[fold]
	if l == nil {
		return
	}
	i := l.find(MessageGUID(p.net.ID, fold, msgid))
	if i < 0 {
		return
	}
	if reactionID != "" {
		p.reactions[reactionID] = struct{}{}
	}
	// A new map, never a write into the old one: Messages handed that one
	// out, and the UI may be reading it.
	counts := make(map[string]int, len(l.messages[i].ReactionCounts)+1)
	for k, v := range l.messages[i].ReactionCounts {
		counts[k] = v
	}
	counts[emoji]++
	l.messages[i].ReactionCounts = counts
}

// onMembership tracks which channels the connection is in, and who is in
// each — what nick completion offers. The user's own JOIN starts a channel;
// everyone else's JOIN, PART, KICK, QUIT and NICK keep its member list
// current between the NAMES the server sends on joining.
func (p *Provider) onMembership(line ircmsg.Message) {
	if len(line.Params) < 1 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.session
	nick := line.Nick()
	switch line.Command {
	case "JOIN":
		for _, name := range strings.Split(line.Params[0], ",") {
			l := p.logFor(name, true)
			if p.isMe(nick) {
				p.joined[s.mapping.fold(name)] = true
				continue
			}
			l.addMember(s.mapping, nick)
		}
	case "PART":
		for _, name := range strings.Split(line.Params[0], ",") {
			if p.isMe(nick) {
				delete(p.joined, s.mapping.fold(name))
			} else if l := p.logs[s.mapping.fold(name)]; l != nil {
				l.removeMember(s.mapping, nick)
			}
		}
	case "KICK":
		if len(line.Params) < 2 {
			return
		}
		if p.isMe(line.Params[1]) {
			delete(p.joined, s.mapping.fold(line.Params[0]))
		} else if l := p.logs[s.mapping.fold(line.Params[0])]; l != nil {
			l.removeMember(s.mapping, line.Params[1])
		}
	case "QUIT":
		for _, l := range p.logs {
			l.removeMember(s.mapping, nick)
		}
	case "NICK":
		for _, l := range p.logs {
			l.renameMember(s.mapping, nick, line.Params[0])
		}
	case ircevent.RPL_NAMREPLY:
		// <me> <symbol> <channel> :<names>
		if len(line.Params) < 4 {
			return
		}
		l := p.logFor(line.Params[2], true)
		if l.namesPending == nil {
			l.namesPending = make(map[string]string)
		}
		for _, entry := range strings.Fields(line.Params[3]) {
			// multi-prefix gives "@+nick", userhost-in-names "nick!u@h".
			entry = strings.TrimLeft(entry, s.prefixes)
			entry, _, _ = strings.Cut(entry, "!")
			if entry != "" {
				l.namesPending[s.mapping.fold(entry)] = entry
			}
		}
	case ircevent.RPL_ENDOFNAMES:
		// <me> <channel> :End of /NAMES list
		if len(line.Params) < 2 {
			return
		}
		if l := p.logs[s.mapping.fold(line.Params[1])]; l != nil && l.namesPending != nil {
			l.members, l.namesPending = l.namesPending, nil
		}
	}
}

// onMarkRead takes the read marker another client — or this one — set.
func (p *Provider) onMarkRead(line ircmsg.Message) {
	if len(line.Params) < 2 {
		return
	}
	value, ok := strings.CutPrefix(line.Params[1], "timestamp=")
	if !ok {
		return // "*": no marker yet
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	name := line.Params[0]
	l := p.logFor(name, p.session.isChannel(name))
	if millis := t.UnixMilli(); !l.readKnown || millis > l.lastRead {
		l.lastRead, l.readKnown = millis, true
	}
}

// onFail handles a standard-reply failure. A failed CHATHISTORY will never be
// answered with a batch, so whatever waits for one is released, and the
// conversation is not asked again until the next reconnect.
func (p *Provider) onFail(line ircmsg.Message) {
	if len(line.Params) < 1 || line.Params[0] != "CHATHISTORY" {
		return
	}
	log.Printf("[irc] %s: chathistory failed: %s", p.ID(), strings.Join(line.Params[1:], " "))
	p.mu.Lock()
	var keys []string
	for key := range p.waiters {
		if fold, ok := strings.CutPrefix(key, "history "); ok {
			if l := p.logs[fold]; l != nil {
				l.filled = true
			}
			keys = append(keys, key)
		} else if key == "targets" {
			p.targets = true
			keys = append(keys, key)
		}
	}
	p.mu.Unlock()
	for _, key := range keys {
		p.release(key)
	}
}

// onBatch takes the batches this client asked for: a conversation's history
// and the list of conversations with history. Anything else is left to the
// library, which hands its lines to the ordinary callbacks one by one.
func (p *Provider) onBatch(batch *ircevent.Batch) bool {
	if batch == nil || len(batch.Params) < 2 {
		return false
	}
	switch strings.TrimPrefix(batch.Params[1], "draft/") {
	case "chathistory":
		if len(batch.Params) < 3 {
			return false
		}
		p.onHistory(batch.Params[2], batch.Items)
		return true
	case "chathistory-targets":
		p.onTargets(batch.Items)
		return true
	}
	return false
}

func (p *Provider) onHistory(target string, items []*ircevent.Batch) {
	for _, item := range items {
		if item == nil {
			continue
		}
		switch item.Command {
		case "PRIVMSG", "NOTICE":
			p.onMessage(item.Message, false)
		case "TAGMSG":
			p.onTagmsg(item.Message)
		}
	}
	p.mu.Lock()
	fold := p.session.mapping.fold(target)
	p.logFor(target, p.session.isChannel(target)).filled = true
	p.mu.Unlock()
	p.release("history " + fold)
}

func (p *Provider) onTargets(items []*ircevent.Batch) {
	p.mu.Lock()
	for _, item := range items {
		// CHATHISTORY TARGETS <target> <timestamp>
		if item == nil || item.Command != "CHATHISTORY" || len(item.Params) < 3 || item.Params[0] != "TARGETS" {
			continue
		}
		name := item.Params[1]
		l := p.logFor(name, p.session.isChannel(name))
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(item.Params[2], "timestamp=")); err == nil {
			if millis := t.UnixMilli(); millis > l.activeAt {
				l.activeAt = millis
			}
		}
	}
	p.targets = true
	p.mu.Unlock()
	p.release("targets")
}

// outgoing is a message that had to be sent as several lines, waiting for
// their echoes.
//
// The UI shows what was typed as one optimistic message and matches the
// server's copy against it by text. Shown as the separate lines IRC carried,
// that copy would never match: the optimistic one would sit there for its
// whole timeout beside them. So the echoes are held until all have arrived,
// then become one message with the typed text. History brings the lines back
// separately later; the ids of all but the first are kept in absorbed so they
// are not added a second time.
type outgoing struct {
	target string
	pieces []string
	text   string
	parts  []models.Message
	at     time.Time
}

// absorbEcho folds an echo of the user's own line into the outgoing message
// it belongs to. held says the echo was taken; out is what is now ready to
// store and publish — the whole message once its last piece is in, and the
// pieces of any message that waited too long, each on its own. Called with
// p.mu held.
func (p *Provider) absorbEcho(fold string, msg models.Message, now time.Time) (out []models.Message, held bool) {
	out = p.pruneOutgoing(now)
	for i, o := range p.outgoing {
		if o.target != fold || msg.Format != "" || sanitize(o.pieces[len(o.parts)]) != msg.Text {
			continue
		}
		o.parts = append(o.parts, msg)
		if len(o.parts) < len(o.pieces) {
			return out, true
		}
		whole := o.parts[0]
		whole.Text = o.text
		for _, part := range o.parts[1:] {
			p.absorbed[messageID(part.GUID)] = struct{}{}
		}
		p.outgoing = append(p.outgoing[:i], p.outgoing[i+1:]...)
		return append(out, whole), true
	}
	return out, false
}

// pruneOutgoing gives up on messages whose echoes stopped coming and returns
// the pieces that did arrive, to be stored and shown as they are. Called with
// p.mu held.
func (p *Provider) pruneOutgoing(now time.Time) []models.Message {
	var out []models.Message
	kept := p.outgoing[:0]
	for _, o := range p.outgoing {
		if now.Sub(o.at) < outgoingTimeout {
			kept = append(kept, o)
			continue
		}
		out = append(out, o.parts...)
	}
	p.outgoing = kept
	return out
}
