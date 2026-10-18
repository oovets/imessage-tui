package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// TelegramAccount is one logged-in Telegram account.
//
// Everything here lives in the OS keyring, in one entry holding every account,
// as the Slack workspaces do. Session is the secret: it holds the auth keys,
// and whoever has it is logged in as the account. The API id and hash belong
// to the user's own application registration at my.telegram.org; they are kept
// beside the session so a normal start needs no environment.
type TelegramAccount struct {
	// ID is the account's numeric Telegram user id. It is part of every chat
	// GUID, and unlike the phone number or the name it never changes.
	ID      string `json:"id"`
	Name    string `json:"name"`
	APIID   int    `json:"apiId"`
	APIHash string `json:"apiHash"`
	Session []byte `json:"session"`
}

const telegramKeyringUser = "telegram.accounts"

// telegramMu serialises read-modify-write of the account list. gotd stores a
// session whenever its keys or salts change, from its own goroutine, and two
// accounts doing that at once would otherwise drop one of the writes.
var telegramMu sync.Mutex

// LoadTelegramAccounts returns the logged-in accounts.
func LoadTelegramAccounts() ([]TelegramAccount, error) {
	telegramMu.Lock()
	defer telegramMu.Unlock()
	return loadTelegramAccounts()
}

func loadTelegramAccounts() ([]TelegramAccount, error) {
	blob, err := keyring.Get(keyringService, telegramKeyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var accounts []TelegramAccount
	if err := json.Unmarshal([]byte(blob), &accounts); err != nil {
		return nil, err
	}
	out := accounts[:0]
	for _, account := range accounts {
		account.ID = strings.TrimSpace(account.ID)
		if account.ID == "" || len(account.Session) == 0 {
			continue
		}
		out = append(out, account)
	}
	return out, nil
}

func saveTelegramAccounts(accounts []TelegramAccount) error {
	if len(accounts) == 0 {
		err := keyring.Delete(keyringService, telegramKeyringUser)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	blob, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, telegramKeyringUser, string(blob))
}

// SaveTelegramAccount adds account, or replaces the one with the same id —
// logging in again to an account that is already here refreshes its session
// rather than listing it twice.
func SaveTelegramAccount(account TelegramAccount) error {
	telegramMu.Lock()
	defer telegramMu.Unlock()
	accounts, err := loadTelegramAccounts()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].ID == account.ID {
			accounts[i] = account
			return saveTelegramAccounts(accounts)
		}
	}
	return saveTelegramAccounts(append(accounts, account))
}

// RemoveTelegramAccount forgets an account. Removing one that is not there is
// not an error.
func RemoveTelegramAccount(id string) error {
	telegramMu.Lock()
	defer telegramMu.Unlock()
	accounts, err := loadTelegramAccounts()
	if err != nil {
		return err
	}
	kept := accounts[:0]
	for _, account := range accounts {
		if account.ID != id {
			kept = append(kept, account)
		}
	}
	return saveTelegramAccounts(kept)
}

// TelegramAPICredentials reads the application id and hash for a login from
// the environment: BB_TELEGRAM_API_ID / BB_TELEGRAM_API_HASH like every other
// setting here, or TG_API_ID / TG_API_HASH, the names the desktop app uses.
func TelegramAPICredentials() (int, string, bool) {
	idText := firstEnv("BB_TELEGRAM_API_ID", "TG_API_ID")
	hash := firstEnv("BB_TELEGRAM_API_HASH", "TG_API_HASH")
	id, err := strconv.Atoi(idText)
	if err != nil || id <= 0 || hash == "" {
		return 0, "", false
	}
	return id, hash, true
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// ErrTelegramSessionNotFound is what an account's session store returns when
// the account has no stored session. It is gotd's session.ErrNotFound in
// meaning; the provider translates it, so this package needs no gotd import.
var ErrTelegramSessionNotFound = errors.New("telegram session not found")

// TelegramSessionStore keeps one account's session in the keyring entry. It
// has the shape of gotd's session.Storage.
type TelegramSessionStore struct {
	AccountID string
}

// LoadSession returns the stored session bytes.
func (s TelegramSessionStore) LoadSession(context.Context) ([]byte, error) {
	accounts, err := LoadTelegramAccounts()
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if account.ID == s.AccountID {
			return account.Session, nil
		}
	}
	return nil, ErrTelegramSessionNotFound
}

// StoreSession replaces the account's session. An account that was removed
// meanwhile — logged out from another terminal — stays removed: writing it
// back would resurrect a session the user asked to forget.
func (s TelegramSessionStore) StoreSession(_ context.Context, data []byte) error {
	telegramMu.Lock()
	defer telegramMu.Unlock()
	accounts, err := loadTelegramAccounts()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].ID == s.AccountID {
			accounts[i].Session = append([]byte(nil), data...)
			return saveTelegramAccounts(accounts)
		}
	}
	return nil
}
