package stdlib_test

import (
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/stdlib"
)

func TestEmbeddedActions(t *testing.T) {
	entries, err := stdlib.FS.ReadDir("actions/preflight")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected embedded stdlib actions")
	}
}

func TestAllStdlibActions(t *testing.T) {
	for _, name := range []string{
		"autologin",
		"debloat",
		"git-sync",
		"windows-input",
		"windows-machine",
		"windows-power",
		"windows-quiet-mode",
		"windows-shell",
		"windows-update-lockdown",
	} {
		path := "actions/preflight/" + name + "/action.yml"
		data, err := stdlib.FS.ReadFile(path)
		if err != nil {
			t.Errorf("missing stdlib action %s: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("empty action file: %s", path)
			continue
		}
		if _, err := action.ParseAction(data); err != nil {
			t.Errorf("invalid stdlib action %s: %v", name, err)
		}
	}
}

func TestStdlibHKCURegistryTasksForwardUser(t *testing.T) {
	entries, err := stdlib.FS.ReadDir("actions/preflight")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		data, err := stdlib.FS.ReadFile("actions/preflight/" + name + "/action.yml")
		if err != nil {
			t.Fatalf("read stdlib action %s: %v", name, err)
		}
		a, err := action.ParseAction(data)
		if err != nil {
			t.Fatalf("parse stdlib action %s: %v", name, err)
		}

		for _, task := range a.Tasks {
			params := task.InlineModules["registry"]
			if params == nil {
				continue
			}
			registryPath, _ := params["path"].(string)
			normalizedPath := strings.ToUpper(registryPath)
			if !strings.HasPrefix(normalizedPath, "HKCU:") && !strings.HasPrefix(normalizedPath, "HKEY_CURRENT_USER") {
				continue
			}
			if _, ok := a.Inputs["user"]; !ok {
				t.Fatalf("action %s task %q writes %s without a user input", name, task.Name, registryPath)
			}
			if got := params["user"]; got != "{{ vars.user }}" {
				t.Fatalf("action %s task %q writes %s with user %v, want user passthrough", name, task.Name, registryPath, got)
			}
		}
	}
}

func TestDebloatGamingAndAIAppsAvoidsPersistentXboxWildcard(t *testing.T) {
	data, err := stdlib.FS.ReadFile("actions/preflight/debloat/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	a, err := action.ParseAction(data)
	if err != nil {
		t.Fatal(err)
	}

	for _, task := range a.Tasks {
		if task.Name != "Remove Microsoft gaming and AI apps" {
			continue
		}
		params := task.InlineModules["remove_appx_packages"]
		packages, ok := params["packages"].([]any)
		if !ok {
			t.Fatalf("expected remove_appx_packages packages list in task %q", task.Name)
		}
		for _, pkg := range packages {
			spec, ok := pkg.(map[string]any)
			if !ok {
				t.Fatalf("expected package spec map, got %T", pkg)
			}
			name, _ := spec["name"].(string)
			if name == "Microsoft.Xbox*" {
				t.Fatalf("task %q must not use Microsoft.Xbox*: it also matches non-removable Xbox system components", task.Name)
			}
		}
		return
	}
	t.Fatal("missing Remove Microsoft gaming and AI apps task")
}

func TestDebloatRemovesOneDrive(t *testing.T) {
	data, err := stdlib.FS.ReadFile("actions/preflight/debloat/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	a, err := action.ParseAction(data)
	if err != nil {
		t.Fatal(err)
	}

	var sawAppx bool
	var sawDesktopUninstall bool
	for _, task := range a.Tasks {
		if params := task.InlineModules["remove_appx_packages"]; params != nil {
			packages, ok := params["packages"].([]any)
			if !ok {
				t.Fatalf("expected remove_appx_packages packages list in task %q", task.Name)
			}
			for _, pkg := range packages {
				spec, ok := pkg.(map[string]any)
				if !ok {
					t.Fatalf("expected package spec map, got %T", pkg)
				}
				if spec["name"] == "Microsoft.OneDriveSync" {
					sawAppx = true
				}
			}
		}
		if task.Name == "Uninstall OneDrive desktop client" {
			params := task.InlineModules["powershell"]
			if params == nil {
				t.Fatalf("expected powershell module for task %q", task.Name)
			}
			script, _ := params["script"].(string)
			checkScript, _ := params["check_script"].(string)
			if strings.Contains(script, "OneDriveSetup.exe") && strings.Contains(checkScript, `Microsoft\OneDrive`) {
				sawDesktopUninstall = true
			}
		}
	}

	if !sawAppx {
		t.Fatal("expected debloat to remove Microsoft.OneDriveSync appx package")
	}
	if !sawDesktopUninstall {
		t.Fatal("expected debloat to uninstall OneDrive desktop client")
	}
}
