package target

import (
	"context"
	"errors"
	"testing"
)

// TestBuildRemoteModuleRegistry_VerifiesAgainstCatalog asserts the registry
// builder treats the catalog as the matrix source of truth: a supported entry
// that the catalog does not list for this runtime is rejected (drift guard),
// and every other known module is filled with an unsupported stub.
func TestBuildRemoteModuleRegistry_VerifiesAgainstCatalog(t *testing.T) {
	t.Run("supported entry must be catalog-supported for runtime", func(t *testing.T) {
		// "registry" is BuiltinWindows → not supported on posix-shell. Registering
		// it as supported on posix-shell is drift and must panic.
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic registering a non-catalog-supported module as supported")
			}
		}()
		supported := ModuleRegistry{"registry": unsupportedModule(nil)}
		_ = buildRemoteModuleRegistry(RuntimeKindPOSIXShell, supported, func(m string) error {
			return NewUnsupportedOnRuntimeError(m, RuntimeKindPOSIXShell)
		})
	})

	t.Run("unknown module registration panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for unknown module registration")
			}
		}()
		supported := ModuleRegistry{"not_a_real_module": unsupportedModule(nil)}
		_ = buildRemoteModuleRegistry(RuntimeKindPOSIXShell, supported, func(m string) error {
			return NewUnsupportedOnRuntimeError(m, RuntimeKindPOSIXShell)
		})
	})

	t.Run("unsupported slots filled for catalog modules not supported on runtime", func(t *testing.T) {
		// posix-shell supports file/directory/shell/wait (common). Every Windows-only
		// catalog module must be filled with an unsupported stub carrying the typed
		// error from the callback.
		supported := ModuleRegistry{
			"file":      unsupportedModule(nil),
			"directory": unsupportedModule(nil),
			"shell":     unsupportedModule(nil),
			"wait":      unsupportedModule(nil),
		}
		reg := buildRemoteModuleRegistry(RuntimeKindPOSIXShell, supported, func(m string) error {
			return NewUnsupportedOnRuntimeError(m, RuntimeKindPOSIXShell)
		})
		// Every catalog remote module has an entry.
		for _, name := range CatalogNames(CapabilityRemote) {
			if _, ok := reg.Lookup(name); !ok {
				t.Errorf("registry missing entry for catalog module %q", name)
			}
		}
		// A Windows-only module is present as a stub whose Check surfaces the
		// typed unsupported_on_runtime error naming the supporting runtimes.
		mod, ok := reg.Lookup("registry")
		if !ok {
			t.Fatal("expected registry entry for windows-only module")
		}
		_, err := mod.Check(context.TODO(), nil, nil)
		var mse *ModuleSupportError
		if err == nil {
			t.Fatal("expected unsupported error from stub")
		}
		if !errors.As(err, &mse) {
			t.Fatalf("expected *ModuleSupportError from stub, got %T: %v", err, err)
		}
		if mse.Class != ClassUnsupportedOnRuntime {
			t.Errorf("stub class = %q, want %q", mse.Class, ClassUnsupportedOnRuntime)
		}
		if mse.RuntimeKind != RuntimeKindPOSIXShell {
			t.Errorf("stub runtime = %q, want %q", mse.RuntimeKind, RuntimeKindPOSIXShell)
		}
	})
}
