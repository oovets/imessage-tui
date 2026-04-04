package config

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "bluebubbles-tui"
	keyringUser    = "default"
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
			return "", ErrCredentialsNotFound
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
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
