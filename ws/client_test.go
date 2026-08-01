package ws

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientReconnectsAfterServerDrop(t *testing.T) {
	var mu sync.Mutex
	connections := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		mu.Lock()
		connections++
		n := connections
		mu.Unlock()

		if n == 1 {
			// Drop the first connection so the client must reconnect.
			return
		}
		// On the second connection, simulate the socket.io handshake and push
		// an event so we can confirm the reconnected socket delivers messages.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"test"}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`42["test"]`))
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	select {
	case <-client.Disconnect:
	case <-time.After(3 * time.Second):
		t.Fatal("no disconnect signal after server drop")
	}

	select {
	case <-client.Reconnect:
	case <-time.After(8 * time.Second):
		t.Fatal("no reconnect within timeout (2s backoff + dial)")
	}

	select {
	case event := <-client.Events:
		if event.Type != "test" {
			t.Fatalf("event type = %q, want test", event.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event delivered after reconnect")
	}
}

func TestClientSignalsDisconnectOnceWhileServerUnreachable(t *testing.T) {
	var mu sync.Mutex
	connections := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		mu.Lock()
		connections++
		n := connections
		mu.Unlock()
		if n >= 2 {
			// Refuse every reconnection attempt so the connection stays dead
			// and the reconnect loop has nothing to read from.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")
	client.backoffMin = 5 * time.Millisecond
	client.backoffMax = 20 * time.Millisecond
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// The single dead connection signals Disconnect exactly once.
	select {
	case <-client.Disconnect:
	case <-time.After(3 * time.Second):
		t.Fatal("no disconnect signal after server drop")
	}

	// Give the reconnect loop time to fail several dials; none of them may
	// re-signal Disconnect.
	extra := 0
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-client.Disconnect:
			extra++
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if extra != 0 {
		t.Fatalf("Disconnect signaled %d extra times during a single dead connection", extra)
	}
}
