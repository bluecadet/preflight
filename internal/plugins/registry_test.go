package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/target"
)

func TestBuildRegistry_NoPlugins(t *testing.T) {
	base := module.Registry()
	binaryDir := t.TempDir()
	workingDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registry, loaded, err := plugins.BuildRegistry(base, plugins.Options{
		BinaryDir:  binaryDir,
		WorkingDir: workingDir,
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded plugins = %#v, want none", loaded)
	}
	if len(registry) != len(base) {
		t.Fatalf("registry size = %d, want %d", len(registry), len(base))
	}
	for name, mod := range base {
		got, ok := registry[name]
		if !ok {
			t.Fatalf("missing module %q in registry", name)
		}
		if got != mod {
			t.Fatalf("registry[%q] changed module instance", name)
		}
	}
}

func TestBuildRegistry_PreservesBuiltins(t *testing.T) {
	base := module.Registry()
	t.Setenv("HOME", t.TempDir())

	registry, loaded, err := plugins.BuildRegistry(base, plugins.Options{
		BinaryDir:  t.TempDir(),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded plugins = %#v, want none", loaded)
	}
	for name := range base {
		if _, ok := registry[name]; !ok {
			t.Fatalf("expected built-in module %q in registry", name)
		}
	}
}

func TestBuildRegistry_EmptyBase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, loaded, err := plugins.BuildRegistry(target.ModuleRegistry{}, plugins.Options{
		BinaryDir:  t.TempDir(),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded plugins = %#v, want none", loaded)
	}
	if len(registry) != 0 {
		t.Fatalf("registry = %#v, want empty", registry)
	}
}

func TestBuildRegistry_DiscoveryIsLazy(t *testing.T) {
	base := module.Registry()
	binaryDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	pluginPath := filepath.Join(binaryDir, "preflight-plugin-custom")
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh\necho should-not-run\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", pluginPath, err)
	}

	registry, loaded, err := plugins.BuildRegistry(base, plugins.Options{
		BinaryDir:  binaryDir,
		WorkingDir: workingDir,
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded plugins = %#v, want 1", loaded)
	}
	if loaded[0].Version != "" {
		t.Fatalf("loaded plugin version = %q, want empty", loaded[0].Version)
	}
	if _, ok := registry[loaded[0].Name]; !ok {
		t.Fatalf("expected plugin %q in registry", loaded[0].Name)
	}
}

func TestBuildRegistry_DetectsDuplicatePlugins(t *testing.T) {
	base := module.Registry()
	binaryDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	first := filepath.Join(binaryDir, "preflight-plugin-custom")
	secondDir := filepath.Join(workingDir, "plugins")
	if err := os.MkdirAll(secondDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", secondDir, err)
	}
	second := filepath.Join(secondDir, "preflight-plugin-custom")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	_, _, err := plugins.BuildRegistry(base, plugins.Options{BinaryDir: binaryDir, WorkingDir: workingDir})
	if err == nil {
		t.Fatal("BuildRegistry returned nil error, want duplicate detection")
	}
}

func TestBuildRegistry_DetectsBuiltinConflicts(t *testing.T) {
	base := module.Registry()
	binaryDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	for name := range base {
		path := filepath.Join(binaryDir, "preflight-plugin-"+name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
		break
	}

	_, _, err := plugins.BuildRegistry(base, plugins.Options{BinaryDir: binaryDir, WorkingDir: t.TempDir()})
	if err == nil {
		t.Fatal("BuildRegistry returned nil error, want builtin conflict")
	}
}
