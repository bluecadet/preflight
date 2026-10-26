package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bluecadet/preflight/internal/plugins"
)

func TestBuildRegistry_NoPlugins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, loaded, err := plugins.BuildRegistry(plugins.Options{
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
	binaryDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	pluginPath := filepath.Join(binaryDir, exeName("preflight-plugin-custom"))
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh\necho should-not-run\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", pluginPath, err)
	}

	registry, loaded, err := plugins.BuildRegistry(plugins.Options{
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
	binaryDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	first := filepath.Join(binaryDir, exeName("preflight-plugin-custom"))
	secondDir := filepath.Join(workingDir, "plugins")
	if err := os.MkdirAll(secondDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", secondDir, err)
	}
	second := filepath.Join(secondDir, exeName("preflight-plugin-custom"))
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	_, _, err := plugins.BuildRegistry(plugins.Options{BinaryDir: binaryDir, WorkingDir: workingDir})
	if err == nil {
		t.Fatal("BuildRegistry returned nil error, want duplicate detection")
	}
}

func TestBuildRegistry_DetectsBuiltinConflicts(t *testing.T) {
	binaryDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(binaryDir, exeName("preflight-plugin-file"))
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	_, _, err := plugins.BuildRegistry(plugins.Options{BinaryDir: binaryDir, WorkingDir: t.TempDir()})
	if err == nil {
		t.Fatal("BuildRegistry returned nil error, want builtin conflict")
	}
}
