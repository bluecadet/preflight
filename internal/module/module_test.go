package module_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluecadet/preflight/internal/module"
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
	needed, err := m.Check(context.Background(), map[string]any{
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

func TestFileModule_ApplyCreatesParentDirectories(t *testing.T) {
	reg := module.Registry()
	m := reg["file"]
	dest := filepath.Join(t.TempDir(), "nested", "deeper", "out.txt")

	if err := m.Apply(context.Background(), map[string]any{
		"dest":   dest,
		"ensure": "present",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected created file at %q: %v", dest, err)
	}
}

func TestFileModule_ApplyCopyCreatesParentDirectories(t *testing.T) {
	reg := module.Registry()
	m := reg["file"]
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dest := filepath.Join(root, "nested", "copied.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", src, err)
	}

	if err := m.Apply(context.Background(), map[string]any{
		"src":    src,
		"dest":   dest,
		"ensure": "present",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", dest, err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected copied contents, got %q", string(data))
	}
}

func TestDirectoryModule_Check_Existing(t *testing.T) {
	dir := t.TempDir()
	reg := module.Registry()
	m := reg["directory"]
	needed, err := m.Check(context.Background(), map[string]any{
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
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })

	reg := module.Registry()
	m := reg["shell"]
	needed, err := m.Check(context.Background(), map[string]any{
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
	err := m.Apply(context.Background(), map[string]any{
		"cmd":  "touch",
		"args": []any{out},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected file to be created by shell apply")
	}
}
