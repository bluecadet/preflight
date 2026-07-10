package target

import (
	"errors"
	"testing"
)

func TestPlanRuntimeForTransport(t *testing.T) {
	if kind, ok := PlanRuntimeForTransport(TransportWinRM); !ok || kind != RuntimeKindWindowsPowerShell {
		t.Errorf("winrm: kind=%q ok=%v, want windows-powershell/true", kind, ok)
	}
	if kind, ok := PlanRuntimeForTransport(TransportLocal); !ok {
		t.Errorf("local: ok=%v, want true", ok)
	} else if kind != runtimeKindForLocal() {
		t.Errorf("local: kind=%q, want %q", kind, runtimeKindForLocal())
	}
	if _, ok := PlanRuntimeForTransport(TransportSSH); ok {
		t.Error("ssh: expected ok=false (runtime needs a probe)")
	}
}

func TestValidateModuleForPlan_UnknownModuleFails(t *testing.T) {
	err := ValidateModuleForPlan("nope", RuntimeKindPOSIXShell, true, nil)
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassUnknownModule {
		t.Fatalf("expected unknown_module, got %v", err)
	}
}

func TestValidateModuleForPlan_SSHNameChecksOnly(t *testing.T) {
	// SSH: kindKnown=false. A Windows-only built-in (registry) is not
	// unsupported at plan time — SSH only name-checks. It must be a known
	// module name though.
	if err := ValidateModuleForPlan("registry", "", false, nil); err != nil {
		t.Errorf("ssh name-check: expected nil for known module registry, got %v", err)
	}
	// Unknown still fails on SSH.
	if err := ValidateModuleForPlan("nope", "", false, nil); err == nil {
		t.Error("ssh name-check: expected error for unknown module")
	}
}

func TestValidateModuleForPlan_RuntimeSupportViolationFails(t *testing.T) {
	// A Windows-only built-in on posix-shell is a runtime-support violation.
	err := ValidateModuleForPlan("registry", RuntimeKindPOSIXShell, true, nil)
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassUnsupportedOnRuntime {
		t.Fatalf("expected unsupported_on_runtime, got %v", err)
	}
}

func TestValidateModuleForPlan_SupportedModulePasses(t *testing.T) {
	if err := ValidateModuleForPlan("file", RuntimeKindPOSIXShell, true, nil); err != nil {
		t.Errorf("file on posix-shell: expected nil, got %v", err)
	}
	if err := ValidateModuleForPlan("registry", RuntimeKindWindowsPowerShell, true, nil); err != nil {
		t.Errorf("registry on windows-powershell: expected nil, got %v", err)
	}
}

func TestValidateModuleForPlan_PluginsBypassMatrix(t *testing.T) {
	reg := ModuleRegistry{
		"custom": fakePluggableModule{path: "/tmp/custom-plugin"},
	}
	// A plugin is known and bypasses the runtime matrix on every runtime.
	if err := ValidateModuleForPlan("custom", RuntimeKindPOSIXShell, true, reg); err != nil {
		t.Errorf("plugin on posix-shell: expected nil, got %v", err)
	}
	if err := ValidateModuleForPlan("custom", RuntimeKindWindowsPowerShell, true, reg); err != nil {
		t.Errorf("plugin on windows-powershell: expected nil, got %v", err)
	}
	// SSH: plugin is known, passes name-check.
	if err := ValidateModuleForPlan("custom", "", false, reg); err != nil {
		t.Errorf("plugin on ssh: expected nil, got %v", err)
	}
}
