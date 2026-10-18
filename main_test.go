package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oovets/imessage-tui/config"
)

func TestConnectIMessage(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":200,"message":"Success","data":[]}`))
	}))
	defer up.Close()

	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()

	tests := []struct {
		name        string
		cfg         config.Config
		wantBackend bool
		wantErr     bool
	}{
		// The case that used to stop the app: no BlueBubbles at all. It must
		// yield untyped nils, or the registry would route every unprefixed GUID
		// to a provider that is not there.
		{name: "not configured", cfg: config.Config{}},
		{name: "unreachable", cfg: config.Config{ServerURL: downURL, Password: "secret"}, wantErr: true},
		{name: "reachable", cfg: config.Config{ServerURL: up.URL, Password: "secret"}, wantBackend: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, stream, err := connectIMessage(&tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if (backend != nil) != tt.wantBackend {
				t.Fatalf("backend = %#v, want one %v", backend, tt.wantBackend)
			}
			if (stream != nil) != tt.wantBackend {
				t.Fatalf("stream = %#v, want one %v", stream, tt.wantBackend)
			}
		})
	}
}
