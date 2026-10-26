package target

import (
	"errors"
	"runtime"
	"testing"
)

func TestPlanRuntimeForTransport(t *testing.T) {
	if kind, ok, err := PlanRuntimeForTransport(TransportWinRM); err != nil || !ok || kind != RuntimeKindWindowsPowerShell {
		t.Errorf("winrm: kind=%q ok=%v err=%v, want windows-powershell/true/nil", kind, ok, err)
	}
	kind, ok, err := PlanRuntimeForTransport(TransportLocal)
	if runtime.GOOS == "windows" {
		if err != nil || !ok || kind != RuntimeKindWindowsPowerShell {
			t.Errorf("local: kind=%q ok=%v err=%v, want windows-powershell/true/nil", kind, ok, err)
		}
	} else if err == nil {
		t.Errorf("local on %s: expected error, got kind=%q ok=%v", runtime.GOOS, kind, ok)
	}
	if _, ok, err := PlanRuntimeForTransport(TransportSSH); ok || err != nil {
		t.Errorf("ssh: ok=%v err=%v, want false/nil (runtime needs a probe)", ok, err)
	}
}

func TestPlatformRuntimeKind(t *testing.T) {
	tests := []struct {
		name     string
		platform Platform
		want     RuntimeKind
		wantOK   bool
	}{
		{name: "windows", platform: Platform{OS: OSFamilyWindows, Arch: "amd64"}, want: RuntimeKindWindowsPowerShell, wantOK: true},
		{name: "unknown", platform: Platform{OS: OSFamilyUnknown, Arch: "amd64"}, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.platform.RuntimeKind()
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("RuntimeKind() = %q, %v; want %q, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestValidateModuleForPlan_UnknownModuleFails(t *testing.T) {
	err := ValidateModuleForPlan("nope", RuntimeKindWindowsPowerShell, true, nil)
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

func TestValidateModuleForPlan_SupportedModulePasses(t *testing.T) {
	if err := ValidateModuleForPlan("registry", RuntimeKindWindowsPowerShell, true, nil); err != nil {
		t.Errorf("registry on windows-powershell: expected nil, got %v", err)
	}
}

func TestValidateModuleForPlan_PluginsBypassMatrix(t *testing.T) {
	reg := ModuleRegistry{
		"custom": fakePluggableModule{path: "/tmp/custom-plugin"},
	}
	// A plugin is known and bypasses the runtime matrix on every runtime.
	if err := ValidateModuleForPlan("custom", RuntimeKindWindowsPowerShell, true, reg); err != nil {
		t.Errorf("plugin on windows-powershell: expected nil, got %v", err)
	}
	// SSH: plugin is known, passes name-check.
	if err := ValidateModuleForPlan("custom", "", false, reg); err != nil {
		t.Errorf("plugin on ssh: expected nil, got %v", err)
	}
}
