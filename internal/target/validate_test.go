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

func TestValidateModuleForPlan(t *testing.T) {
	err := ValidateModuleForPlan("nope", nil)
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassUnknownModule {
		t.Fatalf("expected unknown_module, got %v", err)
	}
	if err := ValidateModuleForPlan("registry", nil); err != nil {
		t.Errorf("catalog module: expected nil, got %v", err)
	}
	reg := ModuleRegistry{"custom": fakePluggableModule{path: "/tmp/custom-plugin"}}
	if err := ValidateModuleForPlan("custom", reg); err != nil {
		t.Errorf("plugin: expected nil, got %v", err)
	}
}
