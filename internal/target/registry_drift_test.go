package target

import (
	"testing"
)

// TestBuildRemoteModuleRegistry_VerifiesAgainstCatalog asserts the registry
// builder treats the catalog as the source of truth: registering a module the
// catalog does not know panics, and every known module is filled in.
func TestBuildRemoteModuleRegistry_VerifiesAgainstCatalog(t *testing.T) {
	t.Run("unknown module registration panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for unknown module registration")
			}
		}()
		supported := ModuleRegistry{"not_a_real_module": unsupportedModule(nil)}
		_ = buildRemoteModuleRegistry(RuntimeKindWindowsPowerShell, supported, func(m string) error {
			return NewUnsupportedOnRuntimeError(m, RuntimeKindWindowsPowerShell)
		})
	})

	t.Run("every catalog module has an entry", func(t *testing.T) {
		reg := buildRemoteModuleRegistry(RuntimeKindWindowsPowerShell, ModuleRegistry{}, func(m string) error {
			return NewUnsupportedOnRuntimeError(m, RuntimeKindWindowsPowerShell)
		})
		for _, name := range CatalogNames(CapabilityRemote) {
			if _, ok := reg.Lookup(name); !ok {
				t.Errorf("registry missing entry for catalog module %q", name)
			}
		}
	})
}
