package module_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/modulecatalog"
	"github.com/bluecadet/preflight/internal/target"
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

func TestRegistry_MatchesCatalog(t *testing.T) {
	reg := module.Registry()
	for _, name := range modulecatalog.Names(modulecatalog.CapabilityBuiltinCommon) {
		if _, ok := reg[name]; !ok {
			t.Fatalf("expected common catalog module %q in registry", name)
		}
	}
	for _, name := range modulecatalog.Names(modulecatalog.CapabilityBuiltinWindows) {
		if _, ok := reg[name]; !ok {
			t.Fatalf("expected windows catalog module %q in registry", name)
		}
	}
	for name := range reg {
		if _, ok := targetModules[name]; !ok {
			t.Fatalf("registry contains uncataloged module %q", name)
		}
	}
}

var targetModules = func() map[string]struct{} {
	all := make(map[string]struct{})
	for _, name := range modulecatalog.Names(modulecatalog.CapabilityBuiltinCommon | modulecatalog.CapabilityBuiltinWindows) {
		all[name] = struct{}{}
	}
	return all
}()

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

func TestFileModule_Check_RejectsOwner(t *testing.T) {
	reg := module.Registry()
	m := reg["file"]
	_, err := m.Check(context.Background(), map[string]any{
		"dest":  "/some/path",
		"owner": "admin",
	})
	if err == nil {
		t.Fatal("expected error for owner param, got nil")
	}
	if !strings.Contains(err.Error(), "owner") {
		t.Errorf("expected error to contain %q, got %q", "owner", err.Error())
	}
}

func TestFileModule_Check_RejectsPermissions(t *testing.T) {
	reg := module.Registry()
	m := reg["file"]
	_, err := m.Check(context.Background(), map[string]any{
		"dest":        "/some/path",
		"permissions": "0644",
	})
	if err == nil {
		t.Fatal("expected error for permissions param, got nil")
	}
	if !strings.Contains(err.Error(), "permissions") {
		t.Errorf("expected error to contain %q, got %q", "permissions", err.Error())
	}
}

func TestDirectoryModule_Check_RejectsOwner(t *testing.T) {
	reg := module.Registry()
	m := reg["directory"]
	_, err := m.Check(context.Background(), map[string]any{
		"path":  "/some/path",
		"owner": "admin",
	})
	if err == nil {
		t.Fatal("expected error for owner param, got nil")
	}
	if !strings.Contains(err.Error(), "owner") {
		t.Errorf("expected error to contain %q, got %q", "owner", err.Error())
	}
}

func TestDirectoryModule_Check_RejectsPermissions(t *testing.T) {
	reg := module.Registry()
	m := reg["directory"]
	_, err := m.Check(context.Background(), map[string]any{
		"path":        "/some/path",
		"permissions": "0755",
	})
	if err == nil {
		t.Fatal("expected error for permissions param, got nil")
	}
	if !strings.Contains(err.Error(), "permissions") {
		t.Errorf("expected error to contain %q, got %q", "permissions", err.Error())
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

func TestShellModule_ApplyWithOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: uses sh -c")
	}

	reg := module.Registry()
	raw := reg["shell"]
	sm, ok := raw.(target.StreamingModule)
	if !ok {
		t.Fatal("shell module does not implement ApplyWithOutput")
	}

	var collected []string
	err := sm.ApplyWithOutput(context.Background(), map[string]any{
		"cmd":  "sh",
		"args": []any{"-c", "printf 'line1\\nline2\\n'"},
	}, func(line string) {
		collected = append(collected, line)
	})
	if err != nil {
		t.Fatalf("ApplyWithOutput returned error: %v", err)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(collected), collected)
	}
	if collected[0] != "line1" {
		t.Errorf("expected collected[0]=%q, got %q", "line1", collected[0])
	}
	if collected[1] != "line2" {
		t.Errorf("expected collected[1]=%q, got %q", "line2", collected[1])
	}

	// nil onOutput must not panic.
	err = sm.ApplyWithOutput(context.Background(), map[string]any{
		"cmd":  "sh",
		"args": []any{"-c", "printf 'hello\\n'"},
	}, nil)
	if err != nil {
		t.Fatalf("ApplyWithOutput with nil onOutput returned error: %v", err)
	}
}
