package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSendMessageWithTempGUIDUsesProvidedTempGUID(t *testing.T) {
	var payload map[string]interface{}
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/message/text" {
			t.Fatalf("path = %s, want /api/v1/message/text", r.URL.Path)
		}
		if got, want := r.URL.Query().Get("guid"), "secret"; got != want {
			t.Fatalf("guid query = %s, want %s", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    r,
		}, nil
	})}

	if err := client.SendMessageWithTempGUID("chat-a", "hello", "", "pending-guid"); err != nil {
		t.Fatalf("SendMessageWithTempGUID returned error: %v", err)
	}

	if got, want := payload["tempGuid"], "pending-guid"; got != want {
		t.Fatalf("tempGuid = %v, want %s", got, want)
	}
}
