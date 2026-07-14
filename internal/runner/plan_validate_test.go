package runner

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/target"
)

// planValidationTarget is a minimal target whose only plan-relevant behavior is
// its Transport().
type planValidationTarget struct {
	transport target.Transport
}

func (t *planValidationTarget) Execute(context.Context, string, string, map[string]any, target.ExecutionOptions, bool, target.OutputFunc) (target.Result, error) {
	return target.Result{}, nil
}
func (t *planValidationTarget) Info(context.Context) (target.TargetInfo, error) {
	return target.TargetInfo{Transport: t.transport}, nil
}
func (t *planValidationTarget) Transport() target.Transport { return t.transport }

func singleTaskPlaybook(module string) *action.Playbook {
	return &action.Playbook{
		Name: "test",
		Tasks: []action.Task{
			{
				Name:         "task " + module,
				ModuleName:   module,
				ModuleParams: map[string]any{},
			},
		},
	}
}

func TestPlan_UnknownModuleFailsForAllTransports(t *testing.T) {
	for _, transport := range []target.Transport{target.TransportLocal, target.TransportWinRM, target.TransportSSH} {
		t.Run(string(transport), func(t *testing.T) {
			r := New(&planValidationTarget{transport: transport}, action.Chain{}, Config{})
			_, err := r.Plan(context.Background(), singleTaskPlaybook("totally_made_up"))
			if err == nil {
				t.Fatalf("expected plan error for unknown module on %s, got nil", transport)
			}
			var mse *target.ModuleSupportError
			if !errors.As(err, &mse) || mse.Class != target.ClassUnknownModule {
				t.Fatalf("expected unknown_module on %s, got %v", transport, err)
			}
		})
	}
}

func TestPlan_RuntimeSupportViolationFailsForLocalAndWinRM(t *testing.T) {
	// registry is Windows-only; on a posix-shell local target and on WinRM
	// (windows-powershell) the outcome differs.
	cases := []struct {
		transport target.Transport
		module    string
		wantFail  bool
	}{
		// On WinRM (windows-powershell) every catalog module is supported.
		{target.TransportWinRM, "registry", false},
		// On local POSIX, a Windows-only module is a runtime-support violation.
		// (Skipped on Windows where local runtime is windows-powershell.)
		{target.TransportLocal, "registry", runtime.GOOS != "windows"},
		// A common module passes on local.
		{target.TransportLocal, "file", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.transport)+"_"+tc.module, func(t *testing.T) {
			if tc.transport == target.TransportLocal && runtime.GOOS == "windows" && tc.wantFail {
				t.Skip("local runtime is windows-powershell on Windows hosts")
			}
			r := New(&planValidationTarget{transport: tc.transport}, action.Chain{}, Config{})
			_, err := r.Plan(context.Background(), singleTaskPlaybook(tc.module))
			if tc.wantFail && err == nil {
				t.Fatalf("expected plan error for %q on %s, got nil", tc.module, tc.transport)
			}
			if !tc.wantFail && err != nil {
				t.Fatalf("expected plan success for %q on %s, got %v", tc.module, tc.transport, err)
			}
			if tc.wantFail {
				var mse *target.ModuleSupportError
				if !errors.As(err, &mse) || mse.Class != target.ClassUnsupportedOnRuntime {
					t.Fatalf("expected unsupported_on_runtime, got %v", err)
				}
			}
		})
	}
}

func TestPlan_SSHNameChecksOnly(t *testing.T) {
	// On SSH, a Windows-only module passes plan-time (runtime not known until
	// probe); only unknown names fail.
	r := New(&planValidationTarget{transport: target.TransportSSH}, action.Chain{}, Config{})
	if _, err := r.Plan(context.Background(), singleTaskPlaybook("registry")); err != nil {
		t.Fatalf("ssh plan should not runtime-check registry, got %v", err)
	}
	_, err := r.Plan(context.Background(), singleTaskPlaybook("totally_made_up"))
	if err == nil || !strings.Contains(err.Error(), "not a known module") {
		t.Fatalf("expected unknown-module error on ssh, got %v", err)
	}
}

func TestPlan_StagePlatformOverridesTransportRuntime(t *testing.T) {
	r := New(&planValidationTarget{transport: target.TransportWinRM}, action.Chain{}, Config{
		Phase:         "stage",
		StagePlatform: &target.Platform{OS: target.OSFamilyLinux, Arch: "amd64"},
	})

	_, err := r.Plan(context.Background(), singleTaskPlaybook("registry"))
	if err == nil || !strings.Contains(err.Error(), "not supported on posix-shell") {
		t.Fatalf("expected declared Linux platform to reject registry, got %v", err)
	}
}

func TestPlan_StagePlatformValidatesWithoutTarget(t *testing.T) {
	r := New(nil, action.Chain{}, Config{
		Phase:         "stage",
		StagePlatform: &target.Platform{OS: target.OSFamilyLinux, Arch: "amd64"},
	})

	_, err := r.Plan(context.Background(), singleTaskPlaybook("registry"))
	if err == nil || !strings.Contains(err.Error(), "not supported on posix-shell") {
		t.Fatalf("expected declared Linux platform to reject registry, got %v", err)
	}
}

func TestPlan_PluginBypassesMatrix(t *testing.T) {
	pluginReg := target.ModuleRegistry{
		"custom": fakePluggable{path: "/tmp/custom"},
	}
	for _, transport := range []target.Transport{target.TransportLocal, target.TransportWinRM, target.TransportSSH} {
		t.Run(string(transport), func(t *testing.T) {
			r := New(&planValidationTarget{transport: transport}, action.Chain{}, Config{
				ModuleRegistry: pluginReg,
			})
			if _, err := r.Plan(context.Background(), singleTaskPlaybook("custom")); err != nil {
				t.Fatalf("plugin should bypass matrix on %s, got %v", transport, err)
			}
		})
	}
}

// fakePluggable is a local copy of the target test helper so this package can
// build a controller registry containing a plugin.
type fakePluggable struct{ path string }

func (fakePluggable) Check(context.Context, map[string]any, target.OutputFunc) (target.CheckResult, error) {
	return target.CheckResult{}, nil
}
func (fakePluggable) Apply(context.Context, map[string]any, target.OutputFunc) (target.ApplyResult, error) {
	return target.ApplyResult{}, nil
}
func (f fakePluggable) PluginPath() string                        { return f.path }
func (f fakePluggable) BindTarget(target.TargetOps) target.Module { return f }
