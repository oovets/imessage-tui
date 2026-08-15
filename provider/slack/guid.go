package slack

import (
	"strconv"
	"strings"
)

// Prefix marks every GUID this backend owns. The registry routes on it, and it
// is the only thing that says a conversation is a Slack one — the source is
// derived from the GUID, never stored beside it.
const Prefix = "sl:"

// Workspace ids are slugs, channel ids are Slack's own (C…/D…/G…), and neither
// contains a colon, so ":" is a safe separator.
const sep = ":"

// ChatGUID is sl:<workspace>:<channel>.
func ChatGUID(workspaceID, channelID string) string {
	return Prefix + workspaceID + sep + channelID
}

// MessageGUID is sl:<workspace>:<channel>:<ts>.
//
// The Slack ts is unique per channel and is also the message's address for
// replies and reactions, which is why it is the identity here rather than an
// invented id.
func MessageGUID(workspaceID, channelID, ts string) string {
	return ChatGUID(workspaceID, channelID) + sep + ts
}

// FileGUID is slfile:<workspace>:<file id>. Slack file URLs need the workspace
// token, so attachments are never handed to the system opener as URLs — they
// route back through the provider, which is what this GUID addresses.
func FileGUID(workspaceID, fileID string) string {
	return "slfile:" + workspaceID + sep + fileID
}

// ParseChatGUID splits a chat GUID back into its parts. ok is false for
// anything that is not a Slack chat GUID.
func ParseChatGUID(guid string) (workspaceID, channelID string, ok bool) {
	if !strings.HasPrefix(guid, Prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(guid, Prefix), sep)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ParseMessageGUID splits a message GUID into chat identity plus timestamp.
func ParseMessageGUID(guid string) (workspaceID, channelID, ts string, ok bool) {
	if !strings.HasPrefix(guid, Prefix) {
		return "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(guid, Prefix), sep)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// TSToMillis converts a Slack timestamp to epoch milliseconds.
//
// Slack's ts is "1700000000.123456" — seconds with microseconds — so it is
// parsed as a number, not as a date. Everything above sorts and renders on
// millisecond epochs, so a ts that silently became 0 would send the message to
// 1970 and to the top of the pane.
func TSToMillis(ts string) int64 {
	seconds, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return 0
	}
	return int64(seconds * 1000)
}
