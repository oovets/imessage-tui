package config

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService       = "imessage-tui"
	legacyKeyringService = "bluebubbles-tui"
	keyringUser          = "default"
)

var ErrCredentialsNotFound = errors.New("credentials not found")

func SavePassword(password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrCredentialsNotFound
	}
	return keyring.Set(keyringService, keyringUser, password)
}

func LoadPassword() (string, error) {
	password, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			// Fallback to legacy service name for backward compatibility.
			legacyPassword, legacyErr := keyring.Get(legacyKeyringService, keyringUser)
			if legacyErr != nil {
				if errors.Is(legacyErr, keyring.ErrNotFound) {
					return "", ErrCredentialsNotFound
				}
				return "", legacyErr
			}
			legacyPassword = strings.TrimSpace(legacyPassword)
			if legacyPassword == "" {
				return "", ErrCredentialsNotFound
			}
			// Best-effort migration to new service key.
			_ = keyring.Set(keyringService, keyringUser, legacyPassword)
			return legacyPassword, nil
		}
		return "", err
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return "", ErrCredentialsNotFound
	}
	return password, nil
}

func DeletePassword() error {
	err := keyring.Delete(keyringService, keyringUser)
	_ = keyring.Delete(legacyKeyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
