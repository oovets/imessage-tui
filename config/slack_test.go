package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlackWorkspaceID(t *testing.T) {
	tests := map[string]string{
		"Acme AB":    "acme-ab",
		"acme":       "acme",
		"A Space":    "a-space",
		"Räksmörgås": "rksmrgs", // non-ascii is dropped, not transliterated
		"  padded  ": "padded",
		"--dashes--": "dashes",
		"":           "",
		"work:space": "workspace", // the colon is the GUID separator
	}
	for name, want := range tests {
		if got := SlackWorkspaceID(name); got != want {
			t.Errorf("SlackWorkspaceID(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNormalizeSlackWorkspaces(t *testing.T) {
	got := normalizeSlackWorkspaces([]SlackWorkspace{
		{Name: "Acme AB", Token: " xoxp-1 ", AppToken: " xapp-1 "},
		{Name: "no token"},
		{ID: "given", Name: "Explicit", Token: "xoxp-2"},
		// Two workspaces that slugify the same must not share an id: every
		// chat GUID carries it, so a collision would merge two companies'
		// conversations into one list.
		{Name: "Acme-AB", Token: "xoxp-3"},
	})

	if len(got) != 3 {
		t.Fatalf("got %d workspaces, want 3 (the one without a token is dropped)", len(got))
	}
	if got[0].ID != "acme-ab" || got[0].Token != "xoxp-1" || got[0].AppToken != "xapp-1" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ID != "given" {
		t.Errorf("explicit id was overwritten: %q", got[1].ID)
	}
	if got[2].ID == got[0].ID {
		t.Errorf("colliding slugs both became %q", got[2].ID)
	}
}

func TestNormalizeSlackWorkspacesNamesTheUnnamed(t *testing.T) {
	got := normalizeSlackWorkspaces([]SlackWorkspace{{ID: "acme", Token: "xoxp-1"}})
	if len(got) != 1 || got[0].Name != "acme" {
		t.Fatalf("got %+v, want the id used as the name", got)
	}
}

func TestLoadLegacySlackConfigAcceptsBothTokenSpellings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	blob := `{"workspaces":[
		{"name":"Acme","token":"xoxp-1","app_token":"xapp-1"},
		{"name":"Beta","token":"xoxp-2","appToken":"xapp-2"},
		{"name":"Tokenless"}
	]}`
	if err := os.WriteFile(filepath.Join(home, ".slack_config.json"), []byte(blob), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	got, err := loadLegacySlackConfig()
	if err != nil {
		t.Fatalf("loadLegacySlackConfig: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d workspaces, want 2", len(got))
	}
	if got[0].ID != "acme" || got[0].AppToken != "xapp-1" {
		t.Errorf("snake_case app token not read: %+v", got[0])
	}
	if got[1].ID != "beta" || got[1].AppToken != "xapp-2" {
		t.Errorf("camelCase app token not read: %+v", got[1])
	}
}

func TestLoadLegacySlackConfigMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := loadLegacySlackConfig()
	if err != nil {
		t.Fatalf("missing legacy config reported an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d workspaces from nowhere", len(got))
	}
}

// An env token is an explicit override for one run, so it wins over anything
// stored — and it must not touch the keyring, which would prompt.
func TestLoadSlackWorkspacesPrefersEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BB_SLACK_TOKEN", "xoxp-env")
	t.Setenv("BB_SLACK_APP_TOKEN", "xapp-env")
	t.Setenv("BB_SLACK_WORKSPACE", "Env Space")

	got, imported, err := LoadSlackWorkspaces()
	if err != nil {
		t.Fatalf("LoadSlackWorkspaces: %v", err)
	}
	if imported {
		t.Error("env config reported as an import")
	}
	if len(got) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(got))
	}
	if got[0].ID != "env-space" || got[0].Token != "xoxp-env" || got[0].AppToken != "xapp-env" {
		t.Errorf("workspace = %+v", got[0])
	}
}
