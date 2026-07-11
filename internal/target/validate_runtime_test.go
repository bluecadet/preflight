package target

import (
	"errors"
	"slices"
	"testing"
)

func TestValidateModuleForRuntime_SupportedPasses(t *testing.T) {
	cases := []struct {
		module string
		kind   RuntimeKind
	}{
		{"shell", RuntimeKindPOSIXShell},
		{"file", RuntimeKindPOSIXShell},
		{"registry", RuntimeKindWindowsPowerShell},
		{"service", RuntimeKindWindowsPowerShell},
	}
	for _, tc := range cases {
		if err := ValidateModuleForRuntime(tc.module, tc.kind, nil); err != nil {
			t.Fatalf("ValidateModuleForRuntime(%q,%s) = %v, want nil", tc.module, tc.kind, err)
		}
	}
}

func TestValidateModuleForRuntime_UnsupportedViolates(t *testing.T) {
	// registry/package are Windows-only; on posix-shell they violate. (service is
	// now BuiltinCommon and supported on posix-shell — it no longer belongs here.)
	for _, module := range []string{"registry", "package"} {
		err := ValidateModuleForRuntime(module, RuntimeKindPOSIXShell, nil)
		if err == nil {
			t.Fatalf("expected unsupported error for %q on posix-shell", module)
		}
		var mse *ModuleSupportError
		if !errors.As(err, &mse) || mse.Class != ClassUnsupportedOnRuntime {
			t.Fatalf("expected unsupported_on_runtime for %q, got %v", module, err)
		}
		if mse.RuntimeKind != RuntimeKindPOSIXShell {
			t.Fatalf("runtime kind = %q, want %q", mse.RuntimeKind, RuntimeKindPOSIXShell)
		}
		if !slices.Contains(mse.SupportedRuntimes, RuntimeKindWindowsPowerShell) {
			t.Fatalf("expected windows-powershell in supported runtimes, got %v", mse.SupportedRuntimes)
		}
	}
}

func TestValidateModuleForRuntime_UnknownModule(t *testing.T) {
	err := ValidateModuleForRuntime("totally_made_up", RuntimeKindPOSIXShell, nil)
	if err == nil {
		t.Fatal("expected unknown_module error")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassUnknownModule {
		t.Fatalf("expected unknown_module, got %v", err)
	}
}

func TestValidateModuleForRuntime_PluginBypasses(t *testing.T) {
	pluginReg := ModuleRegistry{
		"custom": fakePluggableModule{path: "/tmp/custom"},
	}
	for _, kind := range []RuntimeKind{RuntimeKindPOSIXShell, RuntimeKindWindowsPowerShell} {
		if err := ValidateModuleForRuntime("custom", kind, pluginReg); err != nil {
			t.Fatalf("plugin should bypass matrix on %s, got %v", kind, err)
		}
	}
}
