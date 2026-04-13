package facts_test

import (
	"context"
	"os"
	"testing"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/target"
)

// stubTarget is a minimal target.Target for testing fact gathering.
type stubTarget struct {
	info             target.TargetInfo
	runPowerShellOut string
	runPowerShellErr error
}

func (s *stubTarget) Execute(_ context.Context, _ string, _ string, _ map[string]any, _ target.ExecutionOptions, _ bool, _ target.OutputFunc) (target.Result, error) {
	return target.Result{}, nil
}

func (s *stubTarget) Info(_ context.Context) (target.TargetInfo, error) { return s.info, nil }
func (s *stubTarget) Transport() target.Transport                       { return s.info.Transport }
func (s *stubTarget) RunPowerShell(_ context.Context, _ string) (string, error) {
	return s.runPowerShellOut, s.runPowerShellErr
}

func TestGather_RemoteNonWindows_EnvIsEmpty(t *testing.T) {
	// A remote non-Windows (SSH-like) target must not leak local env vars.
	remote := &stubTarget{
		info: target.TargetInfo{
			Hostname:  "remote-linux",
			OSVersion: "ubuntu-22.04",
			OSFamily:  target.OSFamilyLinux,
			Transport: target.TransportSSH,
		},
	}
	g := facts.New(remote)
	f, err := g.Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather: unexpected error: %v", err)
	}
	// The env map must not contain keys from the controller's environment.
	if len(f.Env) != 0 {
		t.Errorf("expected empty env for remote non-Windows target, got %d entries", len(f.Env))
	}
}

func TestGather_LocalNonWindows_EnvIsPopulated(t *testing.T) {
	// A local non-Windows target should expose the local environment.
	if err := os.Setenv("PREFLIGHT_TEST_MARKER", "present"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("PREFLIGHT_TEST_MARKER") })

	local := &stubTarget{
		info: target.TargetInfo{
			Hostname:  "local",
			OSVersion: "darwin",
			OSFamily:  target.OSFamilyDarwin,
			Transport: target.TransportLocal,
		},
	}
	g := facts.New(local)
	f, err := g.Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather: unexpected error: %v", err)
	}
	if f.Env["PREFLIGHT_TEST_MARKER"] != "present" {
		t.Errorf("expected PREFLIGHT_TEST_MARKER=present in local env, got %q", f.Env["PREFLIGHT_TEST_MARKER"])
	}
}
