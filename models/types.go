package models

import (
	"encoding/json"
	"time"
)

// Chat represents a conversation thread (1:1 or group)
type Chat struct {
	GUID            string   `json:"guid"`
	DisplayName     string   `json:"displayName"`
	ChatIdentifier  string   `json:"chatIdentifier"` // phone number, email, or group ID
	Participants    []Handle `json:"participants"`
	LastMessage     *Message `json:"lastMessage"`
	UnreadCount     int      `json:"unreadCount"`
	HasNewMessage   bool     `json:"-"` // Set when a new WS message arrives for this chat
	LastMessageText string   `json:"-"` // Preview of latest message (not from API)
}

// GetDisplayName returns a suitable name for the chat
func (c *Chat) GetDisplayName() string {
	// For 1:1 chats, try to use contact name from participants first
	if len(c.Participants) == 1 && c.Participants[0].DisplayName != "" {
		return c.Participants[0].DisplayName
	}
	// Then try the chat's own display name (for group chats)
	if c.DisplayName != "" {
		return c.DisplayName
	}
	// Fall back to chat identifier (phone/email)
	if c.ChatIdentifier != "" {
		return c.ChatIdentifier
	}
	// Last resort: use participant's address
	if len(c.Participants) > 0 && c.Participants[0].Address != "" {
		return c.Participants[0].Address
	}
	return "Unknown"
}

// Handle represents a contact (phone/email)
type Handle struct {
	Address     string `json:"address"`
	DisplayName string `json:"firstName"`
}

// Message represents a single iMessage
type Message struct {
	GUID        string       `json:"guid"`
	Text        string       `json:"text"`
	IsFromMe    bool         `json:"isFromMe"`
	DateCreated int64        `json:"dateCreated"` // milliseconds epoch
	Handle      *Handle      `json:"handle"`      // nil when isFromMe=true
	Attachments []Attachment `json:"attachments"`
	ChatGUID    string       `json:"-"` // injected after parse
}

// ParsedTime returns the message creation time
func (m *Message) ParsedTime() time.Time {
	return time.UnixMilli(m.DateCreated)
}

// Attachment for future image/file support
type Attachment struct {
	GUID       string `json:"guid"`
	MimeType   string `json:"mimeType"`
	FileName   string `json:"transferName"`
	URL        string `json:"url"`
	Path       string `json:"path"`
	PathOnDisk string `json:"originalPath"`
}

// WSEvent is the envelope for WebSocket frames from BlueBubbles
type WSEvent struct {
	Type string          `json:"type"` // "new-message", "updated-message", etc.
	Data json.RawMessage `json:"data"`
}
