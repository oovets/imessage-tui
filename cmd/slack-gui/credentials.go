package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type slackConfigWorkspace struct {
	Name     string `json:"name"`
	Token    string `json:"token"`
	AppToken string `json:"app_token"`
}

type slackConfigFile struct {
	Workspaces      []slackConfigWorkspace `json:"workspaces"`
	ActiveWorkspace int                    `json:"active_workspace"`
}

func resolveSlackCredentials() (string, string, string, error) {
	if token := strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN")); token != "" {
		appToken := strings.TrimSpace(os.Getenv("SLACK_APP_TOKEN"))
		if appToken == "" {
			appToken = strings.TrimSpace(os.Getenv("SLACK_SOCKET_MODE_APP_TOKEN"))
		}
		return token, appToken, "SLACK_BOT_TOKEN", nil
	}
	if token := strings.TrimSpace(os.Getenv("SLACK_TOKEN")); token != "" {
		appToken := strings.TrimSpace(os.Getenv("SLACK_APP_TOKEN"))
		if appToken == "" {
			appToken = strings.TrimSpace(os.Getenv("SLACK_SOCKET_MODE_APP_TOKEN"))
		}
		return token, appToken, "SLACK_TOKEN", nil
	}

	for _, p := range candidateSlackConfigPaths() {
		token, appToken, err := credentialsFromSlackConfig(p)
		if err != nil {
			log.Printf("Skipping Slack config %q: %v", p, err)
			continue
		}
		if token != "" {
			return token, appToken, p, nil
		}
	}

	return "", "", "", fmt.Errorf("no Slack token found; set SLACK_BOT_TOKEN or SLACK_TOKEN, or provide slack_config.json via SLACK_CONFIG_PATH")
}

func resolveSlackToken() (string, string, error) {
	token, _, source, err := resolveSlackCredentials()
	return token, source, err
}

func candidateSlackConfigPaths() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}

	add(os.Getenv("SLACK_CONFIG_PATH"))
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "slack_rust", "slack_config.json"))
		add(filepath.Join(home, ".config", "slack", "slack_config.json"))
		add(filepath.Join(home, "Code", "slack_rust", "config", "slack_config.json"))
	}
	if cwd, err := os.Getwd(); err == nil {
		add(filepath.Join(cwd, "config", "slack_config.json"))
		add(filepath.Join(cwd, "..", "slack_rust", "config", "slack_config.json"))
	}

	return out
}

func credentialsFromSlackConfig(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}

	var cfg slackConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", fmt.Errorf("invalid JSON: %w", err)
	}
	if len(cfg.Workspaces) == 0 {
		return "", "", fmt.Errorf("no workspaces in file")
	}

	idx := cfg.ActiveWorkspace
	if idx < 0 || idx >= len(cfg.Workspaces) {
		idx = 0
	}
	if token := strings.TrimSpace(cfg.Workspaces[idx].Token); token != "" {
		return token, strings.TrimSpace(cfg.Workspaces[idx].AppToken), nil
	}
	for _, ws := range cfg.Workspaces {
		if token := strings.TrimSpace(ws.Token); token != "" {
			return token, strings.TrimSpace(ws.AppToken), nil
		}
	}
	return "", "", fmt.Errorf("no token field found in workspaces")
}
