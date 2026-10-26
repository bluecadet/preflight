package target

import (
	"errors"
	"testing"
)

func TestValidateModuleForRuntime_SupportedPasses(t *testing.T) {
	cases := []struct {
		module string
		kind   RuntimeKind
	}{
		{"shell", RuntimeKindWindowsPowerShell},
		{"file", RuntimeKindWindowsPowerShell},
		{"registry", RuntimeKindWindowsPowerShell},
		{"service", RuntimeKindWindowsPowerShell},
	}
	for _, tc := range cases {
		if err := ValidateModuleForRuntime(tc.module, tc.kind, nil); err != nil {
			t.Fatalf("ValidateModuleForRuntime(%q,%s) = %v, want nil", tc.module, tc.kind, err)
		}
	}
}

func TestValidateModuleForRuntime_UnknownModule(t *testing.T) {
	err := ValidateModuleForRuntime("totally_made_up", RuntimeKindWindowsPowerShell, nil)
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
	for _, kind := range []RuntimeKind{RuntimeKindWindowsPowerShell} {
		if err := ValidateModuleForRuntime("custom", kind, pluginReg); err != nil {
			t.Fatalf("plugin should bypass matrix on %s, got %v", kind, err)
		}
	}
}
