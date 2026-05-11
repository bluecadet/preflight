package facts_test

import (
	"context"
	"os"
	"testing"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
)

func TestGather_RemoteNonWindows_EnvIsEmpty(t *testing.T) {
	// A remote non-Windows (SSH-like) target must not leak local env vars.
	remote := &targettest.Fake{
		InfoValue: target.TargetInfo{
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

	local := &targettest.Fake{
		InfoValue: target.TargetInfo{
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
