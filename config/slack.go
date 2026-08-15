package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zalando/go-keyring"
)

// SlackWorkspace is one configured Slack login.
//
// Both tokens are secrets and neither is ever written to the config file: they
// live in the OS keyring, in a single entry holding every workspace. One entry
// rather than one per workspace keeps the number of keyring prompts at one.
type SlackWorkspace struct {
	// ID is the slug that appears in chat GUIDs. It must stay stable, so it is
	// derived from the name once and then stored, not recomputed.
	ID       string `json:"id"`
	Name     string `json:"name"`
	Token    string `json:"token"`
	AppToken string `json:"appToken"`
}

// slackKeyringUser is the account name under which the workspace list is
// stored, inside the same keyring service the BlueBubbles password uses.
const slackKeyringUser = "slack.workspaces"

// legacySlackConfigName is the plaintext config the earlier Rust Slack client
// wrote, and which the desktop app also imports from.
const legacySlackConfigName = ".slack_config.json"

// LoadSlackWorkspaces returns the configured workspaces.
//
// Order of precedence: environment first, because an env var is an explicit
// override for one run; then the keyring; then a one-time import of the legacy
// plaintext config, which is migrated into the keyring so it only happens once.
// imported reports that migration, so the caller can tell the user the
// plaintext file is now safe to delete.
func LoadSlackWorkspaces() (workspaces []SlackWorkspace, imported bool, err error) {
	if ws, ok := slackWorkspaceFromEnv(); ok {
		return []SlackWorkspace{ws}, false, nil
	}

	stored, err := loadSlackFromKeyring()
	if err != nil {
		return nil, false, err
	}
	if len(stored) > 0 {
		return stored, false, nil
	}

	legacy, err := loadLegacySlackConfig()
	if err != nil || len(legacy) == 0 {
		return nil, false, err
	}
	if saveErr := SaveSlackWorkspaces(legacy); saveErr != nil {
		// The tokens still work for this run; they just did not make it into
		// the keyring, so the import will be retried next time.
		return legacy, false, nil
	}
	return legacy, true, nil
}

// SaveSlackWorkspaces writes the workspace list to the keyring.
func SaveSlackWorkspaces(workspaces []SlackWorkspace) error {
	if len(workspaces) == 0 {
		err := keyring.Delete(keyringService, slackKeyringUser)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	blob, err := json.Marshal(workspaces)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, slackKeyringUser, string(blob))
}

// LegacySlackConfigPath is where the plaintext config lives, for telling the
// user which file to delete after an import.
func LegacySlackConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return legacySlackConfigName
	}
	return filepath.Join(home, legacySlackConfigName)
}

func slackWorkspaceFromEnv() (SlackWorkspace, bool) {
	token := strings.TrimSpace(os.Getenv("BB_SLACK_TOKEN"))
	if token == "" {
		return SlackWorkspace{}, false
	}
	name := strings.TrimSpace(os.Getenv("BB_SLACK_WORKSPACE"))
	if name == "" {
		name = "slack"
	}
	return SlackWorkspace{
		ID:       SlackWorkspaceID(name),
		Name:     name,
		Token:    token,
		AppToken: strings.TrimSpace(os.Getenv("BB_SLACK_APP_TOKEN")),
	}, true
}

func loadSlackFromKeyring() ([]SlackWorkspace, error) {
	blob, err := keyring.Get(keyringService, slackKeyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var workspaces []SlackWorkspace
	if err := json.Unmarshal([]byte(blob), &workspaces); err != nil {
		return nil, err
	}
	return normalizeSlackWorkspaces(workspaces), nil
}

// legacySlackConfig is the on-disk shape written by the earlier Rust client.
// Its app token key is snake_case; the keyring format uses camelCase, so both
// spellings are accepted here.
type legacySlackConfig struct {
	Workspaces []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Token       string `json:"token"`
		AppToken    string `json:"app_token"`
		AppTokenAlt string `json:"appToken"`
	} `json:"workspaces"`
}

func loadLegacySlackConfig() ([]SlackWorkspace, error) {
	data, err := os.ReadFile(LegacySlackConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var parsed legacySlackConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	workspaces := make([]SlackWorkspace, 0, len(parsed.Workspaces))
	for _, entry := range parsed.Workspaces {
		appToken := entry.AppToken
		if appToken == "" {
			appToken = entry.AppTokenAlt
		}
		workspaces = append(workspaces, SlackWorkspace{
			ID:       entry.ID,
			Name:     entry.Name,
			Token:    entry.Token,
			AppToken: appToken,
		})
	}
	return normalizeSlackWorkspaces(workspaces), nil
}

// normalizeSlackWorkspaces fills in missing ids and drops entries with no
// token. Ids matter more than they look: they are baked into every chat GUID,
// so two workspaces colliding on one would merge their conversations.
func normalizeSlackWorkspaces(workspaces []SlackWorkspace) []SlackWorkspace {
	seen := make(map[string]struct{}, len(workspaces))
	out := make([]SlackWorkspace, 0, len(workspaces))
	for _, ws := range workspaces {
		ws.Name = strings.TrimSpace(ws.Name)
		ws.Token = strings.TrimSpace(ws.Token)
		ws.AppToken = strings.TrimSpace(ws.AppToken)
		if ws.Token == "" {
			continue
		}
		ws.ID = strings.TrimSpace(ws.ID)
		if ws.ID == "" {
			ws.ID = SlackWorkspaceID(ws.Name)
		}
		if ws.ID == "" {
			continue
		}
		if _, clash := seen[ws.ID]; clash {
			ws.ID = uniqueSlackID(ws.ID, seen)
		}
		seen[ws.ID] = struct{}{}
		if ws.Name == "" {
			ws.Name = ws.ID
		}
		out = append(out, ws)
	}
	return out
}

// SlackWorkspaceID slugifies a workspace name into something safe for a GUID.
// Colons are the GUID separator, so they must not survive.
func SlackWorkspaceID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ' ':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueSlackID(base string, taken map[string]struct{}) string {
	for i := 2; ; i++ {
		candidate := base + "-" + strconv.Itoa(i)
		if _, clash := taken[candidate]; !clash {
			return candidate
		}
	}
}
