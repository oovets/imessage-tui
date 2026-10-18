package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func jsonResponse(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

// fakeServer answers chat/query with chats, contact/query with nothing, and
// records every path asked for.
type fakeServer struct {
	t       *testing.T
	chats   string
	mu      sync.Mutex
	paths   []string
	payload map[string]any
}

func (f *fakeServer) client() *Client {
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/chat/query":
			if err := json.NewDecoder(r.Body).Decode(&f.payload); err != nil {
				f.t.Fatalf("chat query payload: %v", err)
			}
			return jsonResponse(r, `{"data":`+f.chats+`}`), nil
		case r.URL.Path == "/api/v1/contact/query":
			return jsonResponse(r, `{"data":[]}`), nil
		case strings.HasSuffix(r.URL.Path, "/message"):
			return jsonResponse(r, `{"data":[{"guid":"m","text":"late","dateCreated":7}]}`), nil
		}
		f.t.Fatalf("unexpected request %s", r.URL.Path)
		return nil, nil
	})}
	return client
}

func chatGUIDs(t *testing.T, client *Client, limit int) string {
	t.Helper()
	chats, err := client.GetChats(limit)
	if err != nil {
		t.Fatalf("GetChats: %v", err)
	}
	var guids []string
	for _, chat := range chats {
		guids = append(guids, chat.GUID)
	}
	return strings.Join(guids, ",")
}

// Measured on a real server: 576 chats, 319 from unknown senders and 91 with
// no messages left. One sorted query with the last message embedded replaces
// a message lookup per chat, and the list it yields is people first.
func TestGetChatsKeepsKnownSendersAndDropsEmptyChats(t *testing.T) {
	server := &fakeServer{t: t, chats: `[
		{"guid":"code","isFiltered":true,"lastMessage":{"text":"123456","dateCreated":60}},
		{"guid":"bank","isFiltered":true,"lastMessage":{"text":"receipt","dateCreated":50}},
		{"guid":"nora","isFiltered":false,"lastMessage":{"text":"hej","dateCreated":40}},
		{"guid":"deleted","isFiltered":false},
		{"guid":"mormor","isFiltered":false,"lastMessage":{"text":"puss","dateCreated":30}},
		{"guid":"old","isFiltered":false,"lastMessage":{"text":"2019","dateCreated":10}}
	]`}
	client := server.client()

	if got, want := chatGUIDs(t, client, 2), "code,bank,nora,mormor"; got != want {
		t.Fatalf("chats = %s, want %s: two known senders, two unknown, no empty chat", got, want)
	}

	with, _ := server.payload["with"].([]any)
	if len(with) != 1 || with[0] != "lastmessage" || server.payload["sort"] != "lastmessage" {
		t.Fatalf("payload = %v, want the last message embedded and sorted on", server.payload)
	}
	for _, path := range server.paths {
		if strings.HasSuffix(path, "/message") {
			t.Fatalf("asked %s: the last message came with the chat, no lookup needed", path)
		}
	}
}

func TestGetChatsFillsPreviewFromTheLastMessage(t *testing.T) {
	server := &fakeServer{t: t, chats: `[{"guid":"nora","lastMessage":{"text":"hej","dateCreated":40}}]`}
	chats, err := server.client().GetChats(10)
	if err != nil {
		t.Fatalf("GetChats: %v", err)
	}
	if len(chats) != 1 || chats[0].LastMessageText != "hej" || chats[0].LastMessageTime != 40 {
		t.Fatalf("chats = %+v, want preview hej at 40", chats)
	}
	if chats[0].UnknownSender {
		t.Fatal("a chat without isFiltered was marked as from an unknown sender")
	}
}

// A server that ignores "with: lastmessage" sends none; the list then falls
// back to looking each chat's activity up rather than coming back empty.
func TestGetChatsFallsBackWhenTheServerSendsNoLastMessage(t *testing.T) {
	server := &fakeServer{t: t, chats: `[{"guid":"a"},{"guid":"b"}]`}

	if got := chatGUIDs(t, server.client(), 10); got != "a,b" && got != "b,a" {
		t.Fatalf("chats = %s, want both chats from the fallback", got)
	}
	lookups := 0
	for _, path := range server.paths {
		if strings.HasSuffix(path, "/message") {
			lookups++
		}
	}
	if lookups != 2 {
		t.Fatalf("message lookups = %d, want one per chat", lookups)
	}
}
