package irc

import (
	"crypto/sha1"
	"encoding/hex"
	"strconv"
	"strings"
)

// Prefix marks every GUID this backend owns, the way sl: marks Slack's and tg:
// Telegram's.
const Prefix = "irc:"

// Network ids are slugs. Channel names and nicks cannot contain a colon (RFC
// 2812 leaves it out of both), so ":" is a safe separator up to the target.
// A msgid is the server's own token and may contain anything but a space, so
// it is always the last part and never split.
const sep = ":"

// syntheticMark starts the id of a message that arrived without a server
// msgid. Such an id is ours alone: it cannot be replied to or reacted to,
// because the server has nothing to look it up by.
const syntheticMark = "~"

// ChatGUID is irc:<network>:<target>, the target casefolded so that #Go and
// #go — one channel to the server — are one conversation here too.
func ChatGUID(networkID, foldedTarget string) string {
	return Prefix + networkID + sep + foldedTarget
}

// MessageGUID is irc:<network>:<target>:<msgid>.
func MessageGUID(networkID, foldedTarget, msgid string) string {
	return ChatGUID(networkID, foldedTarget) + sep + msgid
}

// ParseChatGUID splits a chat GUID back into its parts. ok is false for
// anything that is not an IRC chat GUID.
func ParseChatGUID(guid string) (networkID, target string, ok bool) {
	if !strings.HasPrefix(guid, Prefix) {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(guid, Prefix), sep, 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ParseMessageGUID splits a message GUID into chat identity plus msgid.
func ParseMessageGUID(guid string) (networkID, target, msgid string, ok bool) {
	if !strings.HasPrefix(guid, Prefix) {
		return "", "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(guid, Prefix), sep, 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// syntheticID names a message the server gave no msgid. It is derived from
// what the message says, so the same line seen twice gets the same id and the
// UI's dedupe recognises it.
func syntheticID(millis int64, nick, text string) string {
	sum := sha1.Sum([]byte(strconv.FormatInt(millis, 10) + "\x00" + nick + "\x00" + text))
	return syntheticMark + hex.EncodeToString(sum[:10])
}

// isSynthetic reports whether msgid was made up here rather than sent by the
// server.
func isSynthetic(msgid string) bool { return strings.HasPrefix(msgid, syntheticMark) }

// caseMapping is how a server says two names are the same name. It arrives
// in ISUPPORT CASEMAPPING; rfc1459 is the default when the server says nothing.
type caseMapping int

const (
	mapRFC1459 caseMapping = iota
	mapRFC1459Strict
	mapASCII
)

func parseCaseMapping(value string) caseMapping {
	switch strings.ToLower(value) {
	case "ascii":
		return mapASCII
	case "rfc1459-strict", "strict-rfc1459":
		return mapRFC1459Strict
	default:
		return mapRFC1459
	}
}

// fold is a name as the server compares it. rfc1459 treats []\~ as the upper
// case of {}|^, from the Scandinavian ASCII variants IRC grew up with; strict
// leaves ~ and ^ alone. Anything beyond ASCII is left as it is, which is what
// servers do too.
func (m caseMapping) fold(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		case m == mapASCII:
		case c == '[':
			c = '{'
		case c == ']':
			c = '}'
		case c == '\\':
			c = '|'
		case c == '~' && m == mapRFC1459:
			c = '^'
		}
		b.WriteByte(c)
	}
	return b.String()
}
