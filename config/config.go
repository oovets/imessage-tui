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
	viper.SetConfigName("bluebubbles")
	viper.SetConfigType("yaml")
	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(filepath.Join(home, ".config", "bluebubbles-tui"))
	}
	viper.AddConfigPath(".")

	// Env var bindings
	viper.SetEnvPrefix("BB")
	viper.AutomaticEnv()
	viper.BindEnv("server_url", "BB_SERVER_URL")
	viper.BindEnv("password", "BB_PASSWORD")
	viper.BindEnv("enable_link_previews", "BB_ENABLE_LINK_PREVIEWS")
	viper.BindEnv("max_previews_per_message", "BB_MAX_PREVIEWS_PER_MESSAGE")
	viper.BindEnv("preview_proxy_url", "BB_PREVIEW_PROXY_URL")
	viper.BindEnv("oembed_endpoint", "BB_OEMBED_ENDPOINT")

	// Defaults
	viper.SetDefault("poll_interval_sec", 10)
	viper.SetDefault("message_limit", 50)
	viper.SetDefault("chat_limit", 50)
	viper.SetDefault("enable_link_previews", true)
	viper.SetDefault("max_previews_per_message", 2)
	viper.SetDefault("preview_proxy_url", "")
	viper.SetDefault("oembed_endpoint", "https://noembed.com/embed")

	// Config file is optional
	_ = viper.ReadInConfig()

	cfg := &Config{
		ServerURL:             viper.GetString("server_url"),
		Password:              viper.GetString("password"),
		PollIntervalSec:       viper.GetInt("poll_interval_sec"),
		MessageLimit:          viper.GetInt("message_limit"),
		ChatLimit:             viper.GetInt("chat_limit"),
		EnableLinkPreviews:    viper.GetBool("enable_link_previews"),
		MaxPreviewsPerMessage: viper.GetInt("max_previews_per_message"),
		PreviewProxyURL:       viper.GetString("preview_proxy_url"),
		OEmbedEndpoint:        viper.GetString("oembed_endpoint"),
	}

	if cfg.MaxPreviewsPerMessage < 0 {
		cfg.MaxPreviewsPerMessage = 0
	}

	if cfg.ServerURL == "" || cfg.Password == "" {
		return nil, fmt.Errorf("BB_SERVER_URL and BB_PASSWORD environment variables are required")
	}

	return cfg, nil
}
