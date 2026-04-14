package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInventoryListDisplaysHosts(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.yml")
	if err := os.WriteFile(inventoryPath, []byte(`
groups:
  lab:
    hosts:
      - name: kiosk-a
        address: 10.0.0.1
        transport: winrm
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", inventoryPath, err)
	}

	cmd := newTestCommand()
	if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
		t.Fatalf("Set inventory: %v", err)
	}
	out, err := captureStdout(t, func() error {
		return runInventoryList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runInventoryList: %v", err)
	}
	if !strings.Contains(out, "kiosk-a") {
		t.Fatalf("expected host in inventory list output, got %q", out)
	}
}

func TestRunActionListIncludesEmbeddedActions(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	out, err := captureStdout(t, func() error {
		return runActionList(nil, nil)
	})
	if err != nil {
		t.Fatalf("runActionList: %v", err)
	}
	if !strings.Contains(out, "Embedded actions (preflight/)") {
		t.Fatalf("expected embedded actions header, got %q", out)
	}
	if !strings.Contains(out, "preflight/autologin") {
		t.Fatalf("expected stdlib action in output, got %q", out)
	}
}

func TestRunActionInfoDisplaysStdlibMetadata(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runActionInfo(nil, []string{"preflight/autologin"})
	})
	if err != nil {
		t.Fatalf("runActionInfo: %v", err)
	}
	if !strings.Contains(out, "Name:") || !strings.Contains(out, "Tasks (") {
		t.Fatalf("expected action metadata in output, got %q", out)
	}
}

func TestRunPluginListPrintsStableOutput(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runPluginList(nil, nil)
	})
	if err != nil {
		t.Fatalf("runPluginList: %v", err)
	}
	if !strings.Contains(out, "No plugins found.") && !strings.Contains(out, "NAME") {
		t.Fatalf("expected plugin list output, got %q", out)
	}
}

func TestRunSecretListHandlesEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	out, err := captureStdout(t, func() error {
		return runSecretList(nil, nil)
	})
	if err != nil {
		t.Fatalf("runSecretList: %v", err)
	}
	if !strings.Contains(out, "No secrets configured.") {
		t.Fatalf("expected empty secret list output, got %q", out)
	}
}

func TestRunInventoryListJSONOutput(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.yml")
	if err := os.WriteFile(inventoryPath, []byte(`
groups:
  lab:
    hosts:
      - name: kiosk-a
        address: 10.0.0.1
        transport: winrm
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", inventoryPath, err)
	}

	cmd := newTestCommand()
	if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
		t.Fatalf("Set inventory: %v", err)
	}
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Set output: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runInventoryList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runInventoryList: %v", err)
	}

	var event struct {
		Type  string `json:"type"`
		Hosts []struct {
			Name string `json:"name"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &event); err != nil {
		t.Fatalf("Unmarshal inventory JSON: %v\n%s", err, out)
	}
	if event.Type != "inventory_list" || len(event.Hosts) != 1 || event.Hosts[0].Name != "kiosk-a" {
		t.Fatalf("unexpected inventory JSON output: %+v", event)
	}
}

func TestRunPluginListJSONOutput(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Set output: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runPluginList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runPluginList: %v", err)
	}

	var event struct {
		Type    string `json:"type"`
		Plugins []any  `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &event); err != nil {
		t.Fatalf("Unmarshal plugin JSON: %v\n%s", err, out)
	}
	if event.Type != "plugin_list" {
		t.Fatalf("unexpected plugin JSON output: %+v", event)
	}
}

func TestRunSecretListJSONOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "preflight.yml"), []byte(`
secrets:
  entries:
    api-token:
      file: secrets/api-token.age
`), 0o644); err != nil {
		t.Fatalf("WriteFile(preflight.yml): %v", err)
	}
	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newTestCommand()
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Set output: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runSecretList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runSecretList: %v", err)
	}

	var event struct {
		Type    string `json:"type"`
		Secrets []struct {
			Name string `json:"name"`
			File string `json:"file"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &event); err != nil {
		t.Fatalf("Unmarshal secret JSON: %v\n%s", err, out)
	}
	if event.Type != "secret_list" || len(event.Secrets) != 1 || event.Secrets[0].Name != "api-token" {
		t.Fatalf("unexpected secret JSON output: %+v", event)
	}
}

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q): %v", dir, err)
	}
	return func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("Chdir(%q): %v", cwd, err)
		}
	}
}
