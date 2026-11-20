package irc

import (
	"sort"

	"github.com/oovets/imessage-tui/models"
)

// maxLog is how many messages a conversation keeps. The UI asks for far fewer;
// the rest is headroom so a burst in a busy channel does not push out what a
// pane is showing.
const maxLog = 1000

// chatLog is one conversation as this client knows it.
//
// IRC keeps no history of its own, so this is where Messages reads from.
// Behind a bouncer with chathistory it is filled from the server once per
// connection and kept current by the live feed; on a plain server it holds
// only what arrived since the program started.
type chatLog struct {
	// name is the target as first seen, in the case it was written — what
	// the list shows and what is sent to the server. The map key is the
	// folded form.
	name    string
	channel bool

	messages []models.Message // oldest first

	// filled is whether the server's history was loaded since the
	// connection came up. A reconnect clears it, so the next read fills the
	// gap the drop left.
	filled bool

	// lastRead is the read marker in epoch milliseconds, when the server
	// keeps one (draft/read-marker) or this client set it.
	lastRead  int64
	readKnown bool

	// activeAt is when the conversation last had a message, from CHATHISTORY
	// TARGETS, for ordering the list before its history is loaded.
	activeAt int64

	// members are a channel's nicks, folded to as written; namesPending
	// collects a NAMES reply until its end, which replaces them whole.
	members      map[string]string
	namesPending map[string]string
}

func (l *chatLog) addMember(mapping caseMapping, nick string) {
	if l.members == nil {
		l.members = make(map[string]string)
	}
	l.members[mapping.fold(nick)] = nick
}

func (l *chatLog) removeMember(mapping caseMapping, nick string) {
	delete(l.members, mapping.fold(nick))
}

func (l *chatLog) renameMember(mapping caseMapping, from, to string) {
	if _, ok := l.members[mapping.fold(from)]; !ok {
		return
	}
	delete(l.members, mapping.fold(from))
	l.members[mapping.fold(to)] = to
}

// find returns the index of the message with guid, or -1.
func (l *chatLog) find(guid string) int {
	for i := len(l.messages) - 1; i >= 0; i-- {
		if l.messages[i].GUID == guid {
			return i
		}
	}
	return -1
}

// add stores msg in time order and reports whether it was new.
func (l *chatLog) add(msg models.Message) bool {
	if l.find(msg.GUID) >= 0 {
		return false
	}
	l.messages = append(l.messages, msg)
	if n := len(l.messages); n > 1 && l.messages[n-2].DateCreated > msg.DateCreated {
		sort.SliceStable(l.messages, func(i, j int) bool {
			return l.messages[i].DateCreated < l.messages[j].DateCreated
		})
	}
	if over := len(l.messages) - maxLog; over > 0 {
		l.messages = append([]models.Message(nil), l.messages[over:]...)
	}
	return true
}

// tail returns the newest limit messages, oldest first, as a copy the caller
// may keep: reactions are applied by replacing a message's count map, never by
// writing into one that was handed out.
func (l *chatLog) tail(limit int) []models.Message {
	from := 0
	if limit > 0 && len(l.messages) > limit {
		from = len(l.messages) - limit
	}
	return append([]models.Message{}, l.messages[from:]...)
}

// last is the newest message, or nil.
func (l *chatLog) last() *models.Message {
	if len(l.messages) == 0 {
		return nil
	}
	return &l.messages[len(l.messages)-1]
}

// unread counts other people's messages after the read marker. Without a
// marker nothing is counted: the UI tracks what arrived while it ran, and a
// guess here would light up every channel at startup.
func (l *chatLog) unread() int {
	if !l.readKnown {
		return 0
	}
	n := 0
	for i := len(l.messages) - 1; i >= 0; i-- {
		msg := &l.messages[i]
		if msg.DateCreated <= l.lastRead {
			break
		}
		if !msg.IsFromMe {
			n++
		}
	}
	return n
}

// lastActive is when the conversation last had a message, for ordering.
func (l *chatLog) lastActive() int64 {
	if last := l.last(); last != nil && last.DateCreated > l.activeAt {
		return last.DateCreated
	}
	return l.activeAt
}
