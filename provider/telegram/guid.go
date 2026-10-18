package telegram

import (
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

// Prefix marks every GUID this backend owns, the way sl: marks Slack's.
const Prefix = "tg:"

// filePrefix marks attachment GUIDs. An attachment is reached through its
// message — Telegram's file references expire, so the download re-reads the
// message first — which is why the GUID names the message and not the file.
const filePrefix = "tgfile:"

// Account ids are numbers and peers are a letter plus a number, so ":" is a
// safe separator.
const sep = ":"

// peerKind says which of Telegram's three id spaces a peer id lives in. They
// overlap, and each wants its own input type on the wire, so the kind is part
// of the identity rather than something to look up.
type peerKind byte

const (
	kindUser    peerKind = 'u'
	kindGroup   peerKind = 'g' // basic group
	kindChannel peerKind = 'c' // channel or supergroup
)

// Peer is a chat: a user, a basic group, or a channel or supergroup.
type Peer struct {
	Kind peerKind
	ID   int64
}

func (p Peer) String() string { return string(p.Kind) + strconv.FormatInt(p.ID, 10) }

func parsePeer(s string) (Peer, bool) {
	if len(s) < 2 {
		return Peer{}, false
	}
	kind := peerKind(s[0])
	switch kind {
	case kindUser, kindGroup, kindChannel:
	default:
		return Peer{}, false
	}
	id, err := strconv.ParseInt(s[1:], 10, 64)
	if err != nil || id <= 0 {
		return Peer{}, false
	}
	return Peer{Kind: kind, ID: id}, true
}

// peerOf converts a wire peer.
func peerOf(p tg.PeerClass) (Peer, bool) {
	switch p := p.(type) {
	case *tg.PeerUser:
		return Peer{Kind: kindUser, ID: p.UserID}, true
	case *tg.PeerChat:
		return Peer{Kind: kindGroup, ID: p.ChatID}, true
	case *tg.PeerChannel:
		return Peer{Kind: kindChannel, ID: p.ChannelID}, true
	default:
		return Peer{}, false
	}
}

// ChatGUID is tg:<account>:<peer>.
func ChatGUID(account string, peer Peer) string {
	return Prefix + account + sep + peer.String()
}

// MessageGUID is tg:<account>:<peer>:<message id>.
//
// Message ids are per channel for channels and per account for everything
// else; either way chat plus id is unique.
func MessageGUID(account string, peer Peer, msgID int) string {
	return ChatGUID(account, peer) + sep + strconv.Itoa(msgID)
}

// FileGUID is tgfile:<account>:<peer>:<message id>.
func FileGUID(account string, peer Peer, msgID int) string {
	return filePrefix + account + sep + peer.String() + sep + strconv.Itoa(msgID)
}

// ParseChatGUID splits a chat GUID. It also accepts a message GUID and returns
// the chat it belongs to.
func ParseChatGUID(guid string) (account string, peer Peer, ok bool) {
	parts, ok := split(guid, Prefix, 2)
	if !ok {
		return "", Peer{}, false
	}
	peer, ok = parsePeer(parts[1])
	return parts[0], peer, ok
}

// ParseMessageGUID splits a message GUID.
func ParseMessageGUID(guid string) (account string, peer Peer, msgID int, ok bool) {
	return parseMessageParts(guid, Prefix)
}

func parseFileGUID(guid string) (account string, peer Peer, msgID int, ok bool) {
	return parseMessageParts(guid, filePrefix)
}

func parseMessageParts(guid, prefix string) (string, Peer, int, bool) {
	parts, ok := split(guid, prefix, 3)
	if !ok {
		return "", Peer{}, 0, false
	}
	peer, ok := parsePeer(parts[1])
	if !ok {
		return "", Peer{}, 0, false
	}
	msgID, err := strconv.Atoi(parts[2])
	if err != nil || msgID <= 0 {
		return "", Peer{}, 0, false
	}
	return parts[0], peer, msgID, true
}

// split cuts guid after prefix into at least n non-empty parts.
func split(guid, prefix string, n int) ([]string, bool) {
	if !strings.HasPrefix(guid, prefix) {
		return nil, false
	}
	parts := strings.Split(strings.TrimPrefix(guid, prefix), sep)
	if len(parts) < n {
		return nil, false
	}
	for _, part := range parts[:n] {
		if part == "" {
			return nil, false
		}
	}
	return parts, true
}
