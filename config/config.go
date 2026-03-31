package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	ServerURL             string
	Password              string
	PollIntervalSec       int
	MessageLimit          int
	ChatLimit             int
	EnableLinkPreviews    bool
	MaxPreviewsPerMessage int
	PreviewProxyURL       string
	OEmbedEndpoint        string
}

func Load() (*Config, error) {
	v := newViper()
	if err := v.ReadInConfig(); err != nil {
		_, notFound := err.(viper.ConfigFileNotFoundError)
		if !notFound {
			return nil, err
		}
	}

	cfg := loadFromViper(v)
	if cfg.MaxPreviewsPerMessage < 0 {
		cfg.MaxPreviewsPerMessage = 0
	}

	if cfg.Password == "" {
		if storedPassword, err := LoadPassword(); err == nil {
			cfg.Password = storedPassword
		}
	}

	return cfg, nil
}

func LoadRequired() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if !cfg.HasCredentials() {
		return nil, fmt.Errorf("missing credentials: set BB_SERVER_URL/BB_PASSWORD or run GUI first-run setup")
	}
	return cfg, nil
}

func (c *Config) HasCredentials() bool {
	if c == nil {
		return false
	}
	return c.ServerURL != "" && c.Password != ""
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bluebubbles-tui"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bluebubbles.yaml"), nil
}

func SaveServerURL(serverURL string) error {
	v := newViper()
	_ = v.ReadInConfig()
	v.Set("server_url", serverURL)

	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	v.SetConfigFile(path)
	if err := v.WriteConfigAs(path); err != nil {
		return err
	}
	return nil
}

func SaveCredentials(serverURL, password string) error {
	if err := SaveServerURL(serverURL); err != nil {
		return err
	}
	if err := SavePassword(password); err == nil {
		return nil
	}

	// Fallback for environments without an available keyring backend.
	v := newViper()
	_ = v.ReadInConfig()
	v.Set("server_url", serverURL)
	v.Set("password", password)

	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	v.SetConfigFile(path)
	return v.WriteConfigAs(path)
}

func ClearStoredPassword() error {
	_ = DeletePassword()

	v := newViper()
	_ = v.ReadInConfig()
	v.Set("password", "")

	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	v.SetConfigFile(path)
	return v.WriteConfigAs(path)
}

func newViper() *viper.Viper {
	v := viper.New()
	v.SetConfigName("bluebubbles")
	v.SetConfigType("yaml")
	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(home, ".config", "bluebubbles-tui"))
	}
	v.AddConfigPath(".")

	v.SetEnvPrefix("BB")
	v.AutomaticEnv()
	v.BindEnv("server_url", "BB_SERVER_URL")
	v.BindEnv("password", "BB_PASSWORD")
	v.BindEnv("enable_link_previews", "BB_ENABLE_LINK_PREVIEWS")
	v.BindEnv("max_previews_per_message", "BB_MAX_PREVIEWS_PER_MESSAGE")
	v.BindEnv("preview_proxy_url", "BB_PREVIEW_PROXY_URL")
	v.BindEnv("oembed_endpoint", "BB_OEMBED_ENDPOINT")

	v.SetDefault("poll_interval_sec", 10)
	v.SetDefault("message_limit", 50)
	v.SetDefault("chat_limit", 50)
	v.SetDefault("enable_link_previews", true)
	v.SetDefault("max_previews_per_message", 2)
	v.SetDefault("preview_proxy_url", "")
	v.SetDefault("oembed_endpoint", "https://noembed.com/embed")

	return v
}

func loadFromViper(v *viper.Viper) *Config {
	return &Config{
		ServerURL:             v.GetString("server_url"),
		Password:              v.GetString("password"),
		PollIntervalSec:       v.GetInt("poll_interval_sec"),
		MessageLimit:          v.GetInt("message_limit"),
		ChatLimit:             v.GetInt("chat_limit"),
		EnableLinkPreviews:    v.GetBool("enable_link_previews"),
		MaxPreviewsPerMessage: v.GetInt("max_previews_per_message"),
		PreviewProxyURL:       v.GetString("preview_proxy_url"),
		OEmbedEndpoint:        v.GetString("oembed_endpoint"),
	}
}
