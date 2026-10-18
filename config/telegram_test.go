package config

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestTelegramAccountsRoundTripThroughTheKeyring(t *testing.T) {
	keyring.MockInit()

	first := TelegramAccount{ID: "100", Name: "Anna", APIID: 1, APIHash: "h", Session: []byte("one")}
	if err := SaveTelegramAccount(first); err != nil {
		t.Fatal(err)
	}
	if err := SaveTelegramAccount(TelegramAccount{ID: "200", Name: "Bo", Session: []byte("two")}); err != nil {
		t.Fatal(err)
	}
	// Logging in to the same account again replaces it instead of listing it
	// twice.
	first.Session = []byte("fresh")
	if err := SaveTelegramAccount(first); err != nil {
		t.Fatal(err)
	}

	accounts, err := LoadTelegramAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].ID != "100" || string(accounts[0].Session) != "fresh" {
		t.Fatalf("accounts = %+v", accounts)
	}

	if err := RemoveTelegramAccount("100"); err != nil {
		t.Fatal(err)
	}
	accounts, _ = LoadTelegramAccounts()
	if len(accounts) != 1 || accounts[0].ID != "200" {
		t.Fatalf("after remove = %+v", accounts)
	}
}

func TestTelegramSessionStore(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()

	if err := SaveTelegramAccount(TelegramAccount{ID: "100", Session: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	store := TelegramSessionStore{AccountID: "100"}
	if err := store.StoreSession(ctx, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSession(ctx)
	if err != nil || string(got) != "new" {
		t.Fatalf("LoadSession = %q, %v", got, err)
	}

	// A session written after the account was logged out must not bring the
	// account back.
	if err := RemoveTelegramAccount("100"); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSession(ctx, []byte("late")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSession(ctx); !errors.Is(err, ErrTelegramSessionNotFound) {
		t.Fatalf("LoadSession after logout err = %v, want not found", err)
	}
}

func TestTelegramAPICredentials(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantID   int
		wantHash string
		wantOK   bool
	}{
		{name: "unset"},
		{name: "app names", env: map[string]string{"BB_TELEGRAM_API_ID": "12", "BB_TELEGRAM_API_HASH": "abc"}, wantID: 12, wantHash: "abc", wantOK: true},
		{name: "desktop names", env: map[string]string{"TG_API_ID": "34", "TG_API_HASH": "def"}, wantID: 34, wantHash: "def", wantOK: true},
		{name: "id not a number", env: map[string]string{"TG_API_ID": "x", "TG_API_HASH": "def"}},
		{name: "no hash", env: map[string]string{"TG_API_ID": "34"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"BB_TELEGRAM_API_ID", "BB_TELEGRAM_API_HASH", "TG_API_ID", "TG_API_HASH"} {
				t.Setenv(name, tt.env[name])
			}
			id, hash, ok := TelegramAPICredentials()
			if id != tt.wantID || hash != tt.wantHash || ok != tt.wantOK {
				t.Fatalf("got %d %q %v, want %d %q %v", id, hash, ok, tt.wantID, tt.wantHash, tt.wantOK)
			}
		})
	}
}
