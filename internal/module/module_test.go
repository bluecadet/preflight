package module_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/claytercek/preflight/internal/module"
)

func TestRegistry_NotEmpty(t *testing.T) {
	reg := module.Registry()
	if len(reg) == 0 {
		t.Fatal("expected non-empty registry")
	}
}

func TestRegistry_CoreModulesPresent(t *testing.T) {
	reg := module.Registry()
	for _, name := range []string{"file", "directory", "shell", "powershell", "environment", "wait", "reboot"} {
		if _, ok := reg[name]; !ok {
			t.Errorf("expected module %q in registry", name)
		}
	}
}

func TestFileModule_Check_Missing(t *testing.T) {
	reg := module.Registry()
	m := reg["file"]
	needed, err := m.Check(context.Background(), map[string]interface{}{
		"dest":   "/nonexistent/path/that/does/not/exist",
		"ensure": "present",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !needed {
		t.Error("expected needsChange=true for missing file")
	}
}

func TestDirectoryModule_Check_Existing(t *testing.T) {
	dir := t.TempDir()
	reg := module.Registry()
	m := reg["directory"]
	needed, err := m.Check(context.Background(), map[string]interface{}{
		"path":   dir,
		"ensure": "present",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needed {
		t.Error("expected needsChange=false for existing directory")
	}
}

func TestShellModule_Check_CreatesExists(t *testing.T) {
	f, err := os.CreateTemp("", "shell-creates-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	reg := module.Registry()
	m := reg["shell"]
	needed, err := m.Check(context.Background(), map[string]interface{}{
		"cmd":     "echo",
		"creates": f.Name(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needed {
		t.Error("expected needsChange=false when creates path exists")
	}
}

func TestShellModule_Apply(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")

	reg := module.Registry()
	m := reg["shell"]
	err := m.Apply(context.Background(), map[string]interface{}{
		"cmd":  "touch",
		"args": []interface{}{out},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected file to be created by shell apply")
	}
}
