package plugins_test

import (
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
