package cmd

import (
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
