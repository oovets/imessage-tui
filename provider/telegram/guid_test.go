package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestGUIDsRoundTrip(t *testing.T) {
	peers := []Peer{
		{Kind: kindUser, ID: 42},
		{Kind: kindGroup, ID: 7},
		{Kind: kindChannel, ID: 1234567890123},
	}
	for _, peer := range peers {
		t.Run(peer.String(), func(t *testing.T) {
			account, got, ok := ParseChatGUID(ChatGUID("100", peer))
			if !ok || account != "100" || got != peer {
				t.Fatalf("chat: %q %v %v", account, got, ok)
			}
			account, got, msgID, ok := ParseMessageGUID(MessageGUID("100", peer, 55))
			if !ok || account != "100" || got != peer || msgID != 55 {
				t.Fatalf("message: %q %v %d %v", account, got, msgID, ok)
			}
			// A message GUID names its chat too, which is how Send finds the
			// chat a reply belongs to.
			if _, got, ok := ParseChatGUID(MessageGUID("100", peer, 55)); !ok || got != peer {
				t.Fatalf("chat of message: %v %v", got, ok)
			}
			account, got, msgID, ok = parseFileGUID(FileGUID("100", peer, 55))
			if !ok || account != "100" || got != peer || msgID != 55 {
				t.Fatalf("file: %q %v %d %v", account, got, msgID, ok)
			}
		})
	}
}

func TestParseRejectsOtherGUIDs(t *testing.T) {
	for _, guid := range []string{
		"",
		"iMessage;-;+46700000000",
		"sl:acme:C123",
		"tg:",
		"tg:100",
		"tg:100:",
		"tg:100:x42", // unknown kind
		"tg:100:u",   // no id
		"tg:100:u-1", // not a real id
		"tg::u42",    // no account
		"tgfile:100:u42:1",
	} {
		if _, _, ok := ParseChatGUID(guid); ok {
			t.Errorf("ParseChatGUID(%q) accepted it", guid)
		}
	}
	for _, guid := range []string{"tg:100:u42", "tg:100:u42:", "tg:100:u42:x", "tg:100:u42:0"} {
		if _, _, _, ok := ParseMessageGUID(guid); ok {
			t.Errorf("ParseMessageGUID(%q) accepted it", guid)
		}
	}
}

func TestPeerOfKeepsTheIDSpacesApart(t *testing.T) {
	tests := []struct {
		wire tg.PeerClass
		want Peer
	}{
		{&tg.PeerUser{UserID: 5}, Peer{Kind: kindUser, ID: 5}},
		{&tg.PeerChat{ChatID: 5}, Peer{Kind: kindGroup, ID: 5}},
		{&tg.PeerChannel{ChannelID: 5}, Peer{Kind: kindChannel, ID: 5}},
	}
	for _, tt := range tests {
		got, ok := peerOf(tt.wire)
		if !ok || got != tt.want {
			t.Errorf("peerOf(%T) = %v, %v; want %v", tt.wire, got, ok, tt.want)
		}
	}
}
