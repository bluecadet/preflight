package stdlib_test

import (
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
