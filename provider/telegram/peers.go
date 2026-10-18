package telegram

import (
	"strings"
	"sync"

	"github.com/gotd/td/tg"
)

// peerTable remembers what the API needs to address a chat and what to call
// it.
//
// Users and channels cannot be addressed by id alone: every request also
// carries the access_hash the server handed out with the object, and it only
// ever arrives as a side effect — in the users and chats lists that come with
// dialogs, history and updates. So every one of those passes through apply.
type peerTable struct {
	mu       sync.RWMutex
	selfID   int64
	users    map[int64]userEntry
	groups   map[int64]string
	channels map[int64]channelEntry
}

type userEntry struct {
	hash    int64
	hasHash bool
	name    string
	bot     bool
}

type channelEntry struct {
	hash      int64
	hasHash   bool
	title     string
	broadcast bool
}

func newPeerTable(selfID int64) *peerTable {
	return &peerTable{
		selfID:   selfID,
		users:    make(map[int64]userEntry),
		groups:   make(map[int64]string),
		channels: make(map[int64]channelEntry),
	}
}

// apply records users and chats from a response.
//
// A "min" constructor is a partial object the server sends where the full one
// would be too expensive — a member seen in a big group, say. Its access_hash
// is not valid for addressing the object, so it must never replace a real one;
// its name is still the person's name.
func (t *peerTable) apply(users []tg.UserClass, chats []tg.ChatClass) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, u := range users {
		user, ok := u.(*tg.User)
		if !ok {
			continue
		}
		entry := t.users[user.ID]
		if name := userName(user); name != "" {
			entry.name = name
		}
		entry.bot = user.Bot
		if user.AccessHash != 0 && !user.Min {
			entry.hash, entry.hasHash = user.AccessHash, true
		}
		t.users[user.ID] = entry
	}
	for _, c := range chats {
		switch chat := c.(type) {
		case *tg.Chat:
			t.groups[chat.ID] = chat.Title
		case *tg.ChatForbidden:
			t.groups[chat.ID] = chat.Title
		case *tg.Channel:
			entry := t.channels[chat.ID]
			entry.title = chat.Title
			entry.broadcast = chat.Broadcast
			if chat.AccessHash != 0 && !chat.Min {
				entry.hash, entry.hasHash = chat.AccessHash, true
			}
			t.channels[chat.ID] = entry
		case *tg.ChannelForbidden:
			entry := t.channels[chat.ID]
			entry.title = chat.Title
			entry.broadcast = chat.Broadcast
			entry.hash, entry.hasHash = chat.AccessHash, true
			t.channels[chat.ID] = entry
		}
	}
}

// applyEntities records the users and chats that came with an update.
func (t *peerTable) applyEntities(e tg.Entities) {
	users := make([]tg.UserClass, 0, len(e.Users))
	for _, u := range e.Users {
		users = append(users, u)
	}
	chats := make([]tg.ChatClass, 0, len(e.Chats)+len(e.Channels))
	for _, c := range e.Chats {
		chats = append(chats, c)
	}
	for _, c := range e.Channels {
		chats = append(chats, c)
	}
	t.apply(users, chats)
}

// inputPeer builds the wire address of a chat. ok is false when the chat
// needs an access hash this table has not seen yet.
func (t *peerTable) inputPeer(p Peer) (tg.InputPeerClass, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	switch p.Kind {
	case kindUser:
		if p.ID == t.selfID {
			return &tg.InputPeerSelf{}, true
		}
		entry, ok := t.users[p.ID]
		if !ok || !entry.hasHash {
			return nil, false
		}
		return &tg.InputPeerUser{UserID: p.ID, AccessHash: entry.hash}, true
	case kindGroup:
		// Basic groups are addressed by id alone.
		return &tg.InputPeerChat{ChatID: p.ID}, true
	case kindChannel:
		entry, ok := t.channels[p.ID]
		if !ok || !entry.hasHash {
			return nil, false
		}
		return &tg.InputPeerChannel{ChannelID: p.ID, AccessHash: entry.hash}, true
	}
	return nil, false
}

// inputChannel is the channel-only address some channel methods take instead
// of a peer.
func (t *peerTable) inputChannel(id int64) (*tg.InputChannel, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entry, ok := t.channels[id]
	if !ok || !entry.hasHash {
		return nil, false
	}
	return &tg.InputChannel{ChannelID: id, AccessHash: entry.hash}, true
}

// chatName is what the chat list calls a chat.
func (t *peerTable) chatName(p Peer) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	switch p.Kind {
	case kindUser:
		if p.ID == t.selfID {
			// What every Telegram client calls the chat with yourself.
			return "Saved Messages"
		}
		if name := t.users[p.ID].name; name != "" {
			return name
		}
	case kindGroup:
		if title := t.groups[p.ID]; title != "" {
			return title
		}
	case kindChannel:
		if title := t.channels[p.ID].title; title != "" {
			return title
		}
	}
	return p.String()
}

// senderName names the author of a message: a person, or a channel posting as
// itself (a broadcast post, an anonymous admin).
func (t *peerTable) senderName(p Peer) string {
	if p.Kind == kindUser && p.ID == t.selfID {
		return ""
	}
	return t.chatName(p)
}

func (t *peerTable) isBot(userID int64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.users[userID].bot
}

func (t *peerTable) isBroadcast(channelID int64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.channels[channelID].broadcast
}

// userName is a person's name as Telegram's own clients show it: first and
// last name, falling back to the @username, then the number.
func userName(u *tg.User) string {
	if u.Deleted {
		return "Deleted Account"
	}
	if name := strings.TrimSpace(u.FirstName + " " + u.LastName); name != "" {
		return name
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	if u.Phone != "" {
		return "+" + u.Phone
	}
	return ""
}
