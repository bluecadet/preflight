package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/config"
)

func TestParse_ValidYAML(t *testing.T) {
	data := []byte(`project: museum-kiosk
environment: production
vars:
  site: lobby
  retries: 3
secrets:
  identity: keys/identity.age
  recipients:
    - age1example
  entries:
    db_password:
      file: secrets/db-password.age
      type: age
inventory:
  vars:
    timezone: America/New_York
  groups:
    lobby:
      vars:
        area: lobby
  hosts:
    - name: lobby-pc-01
      transport: local
      groups: [lobby]
`)

	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if cfg.Project != "museum-kiosk" {
		t.Fatalf("Project = %q, want %q", cfg.Project, "museum-kiosk")
	}
	if cfg.Environment != "production" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "production")
	}
	if got := cfg.Vars["site"]; got != "lobby" {
		t.Fatalf("Vars[site] = %#v, want %q", got, "lobby")
	}
	if got := cfg.Vars["retries"]; got != 3 {
		t.Fatalf("Vars[retries] = %#v, want %d", got, 3)
	}
	if cfg.Secrets.Identity != "keys/identity.age" {
		t.Fatalf("Secrets.Identity = %q, want %q", cfg.Secrets.Identity, "keys/identity.age")
	}
	if len(cfg.Secrets.Recipients) != 1 || cfg.Secrets.Recipients[0] != "age1example" {
		t.Fatalf("Secrets.Recipients = %#v, want [age1example]", cfg.Secrets.Recipients)
	}
	entry, ok := cfg.Secrets.Entries["db_password"]
	if !ok {
		t.Fatal("expected db_password secret entry")
	}
	if entry.File != "secrets/db-password.age" {
		t.Fatalf("entry.File = %q, want %q", entry.File, "secrets/db-password.age")
	}
	if entry.Type != "age" {
		t.Fatalf("entry.Type = %q, want %q", entry.Type, "age")
	}
	if cfg.Inventory == nil {
		t.Fatal("Inventory is nil")
	}
	hosts, err := cfg.Inventory.HostsForTarget("lobby-pc-01")
	if err != nil {
		t.Fatalf("HostsForTarget returned error: %v", err)
	}
	if got := hosts[0].Vars["timezone"]; got != "America/New_York" {
		t.Fatalf("Inventory host timezone = %#v, want America/New_York", got)
	}
	if got := hosts[0].Vars["area"]; got != "lobby" {
		t.Fatalf("Inventory host area = %#v, want lobby", got)
	}
}

func TestParse_EmptyYAML(t *testing.T) {
	cfg, err := config.Parse(nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if cfg.Vars == nil {
		t.Fatal("Vars is nil")
	}
	if cfg.Secrets.Entries == nil {
		t.Fatal("Secrets.Entries is nil")
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := config.Parse([]byte("{{{"))
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "config: schema validation parse error") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "config: schema validation parse error")
	}
}

func TestParse_SchemaValidationFailure(t *testing.T) {
	_, err := config.Parse([]byte("vars: []\n"))
	if err == nil {
		t.Fatal("expected schema validation error, got nil")
	}
	if !strings.Contains(err.Error(), "config: schema validation failed") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "config: schema validation failed")
	}
}

func TestParse_NilMaps(t *testing.T) {
	cfg, err := config.Parse([]byte("project: sample\nenvironment: dev\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if cfg.Vars == nil {
		t.Fatal("Vars is nil")
	}
	if len(cfg.Vars) != 0 {
		t.Fatalf("len(Vars) = %d, want 0", len(cfg.Vars))
	}
	if cfg.Secrets.Entries == nil {
		t.Fatal("Secrets.Entries is nil")
	}
	if len(cfg.Secrets.Entries) != 0 {
		t.Fatalf("len(Secrets.Entries) = %d, want 0", len(cfg.Secrets.Entries))
	}
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := config.ParseFile(filepath.Join(t.TempDir(), "missing.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseFile_Valid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preflight.yml")
	data := []byte("project: sample\nvars:\n  enabled: true\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	cfg, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}
	if cfg.Project != "sample" {
		t.Fatalf("Project = %q, want %q", cfg.Project, "sample")
	}
	if got := cfg.Vars["enabled"]; got != true {
		t.Fatalf("Vars[enabled] = %#v, want true", got)
	}
}

func TestLoadOptional_Missing(t *testing.T) {
	cfg, err := config.LoadOptional(filepath.Join(t.TempDir(), "missing.yml"))
	if err != nil {
		t.Fatalf("LoadOptional returned error: %v", err)
	}
	if cfg.Vars == nil {
		t.Fatal("Vars is nil")
	}
	if cfg.Secrets.Entries == nil {
		t.Fatal("Secrets.Entries is nil")
	}
}

func TestLoadOptional_Exists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preflight.yml")
	if err := os.WriteFile(path, []byte("environment: qa\nvars:\n  color: blue\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	cfg, err := config.LoadOptional(path)
	if err != nil {
		t.Fatalf("LoadOptional returned error: %v", err)
	}
	if cfg.Environment != "qa" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "qa")
	}
	if got := cfg.Vars["color"]; got != "blue" {
		t.Fatalf("Vars[color] = %#v, want %q", got, "blue")
	}
}

func TestLoadOptional_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preflight.yml")
	if err := os.WriteFile(path, []byte("{{{"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	_, err := config.LoadOptional(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSaveFile_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "preflight.yml")
	want := &config.Config{
		Project:     "gallery",
		Environment: "staging",
		Vars: map[string]any{
			"site":  "atrium",
			"count": 2,
		},
		Secrets: config.SecretsConfig{
			Identity:   "keys/team.age",
			Recipients: []string{"age1one", "age1two"},
			Entries: map[string]config.SecretEntry{
				"api_key": {
					File: "secrets/api-key.age",
					Type: "age",
				},
			},
		},
	}

	if err := config.SaveFile(path, want); err != nil {
		t.Fatalf("SaveFile returned error: %v", err)
	}

	got, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}

	if got.Project != want.Project || got.Environment != want.Environment {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
	if len(got.Vars) != len(want.Vars) || got.Vars["site"] != want.Vars["site"] || got.Vars["count"] != want.Vars["count"] {
		t.Fatalf("Vars = %#v, want %#v", got.Vars, want.Vars)
	}
	if got.Secrets.Identity != want.Secrets.Identity {
		t.Fatalf("Secrets.Identity = %q, want %q", got.Secrets.Identity, want.Secrets.Identity)
	}
	if len(got.Secrets.Recipients) != len(want.Secrets.Recipients) {
		t.Fatalf("Recipients = %#v, want %#v", got.Secrets.Recipients, want.Secrets.Recipients)
	}
	for i := range want.Secrets.Recipients {
		if got.Secrets.Recipients[i] != want.Secrets.Recipients[i] {
			t.Fatalf("Recipients = %#v, want %#v", got.Secrets.Recipients, want.Secrets.Recipients)
		}
	}
	if got.Secrets.Entries["api_key"] != want.Secrets.Entries["api_key"] {
		t.Fatalf("Entries[api_key] = %#v, want %#v", got.Secrets.Entries["api_key"], want.Secrets.Entries["api_key"])
	}
}

func TestSaveFile_CreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "preflight.yml")
	if err := config.SaveFile(path, &config.Config{Project: "sample"}); err != nil {
		t.Fatalf("SaveFile returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected saved file at %q: %v", path, err)
	}
}

func TestSaveFile_NilConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preflight.yml")
	if err := config.SaveFile(path, nil); err != nil {
		t.Fatalf("SaveFile returned error: %v", err)
	}

	cfg, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Vars == nil {
		t.Fatal("Vars is nil")
	}
	if cfg.Secrets.Entries == nil {
		t.Fatal("Secrets.Entries is nil")
	}
}
