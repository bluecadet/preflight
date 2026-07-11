//go:build integration

package target_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/target"
)

// TestIntegration_POSIXPluginHandle drives the real test-plugin binary
// end-to-end over a real SSH-POSIX target (the CI containers). The plugin
// process runs controller-side (on the CI host) and its handle ops dispatch
// over SSH to the container, proving plugins flow uniformly over local and
// SSH. Covers all four handle ops, streaming output, become-refusal, and
// protocol-version rejection.
//
// Lives in package target_test (not target) so it can import the plugins
// adapter without an import cycle (plugins → target). Uses only exported
// target APIs: NewSSHTarget, ReadFile (oracle + sentinel), Execute.
//
// Matches the CI -run TestIntegration_POSIX filter so it runs on every PR.
func TestIntegration_POSIXPluginHandle(t *testing.T) {
	cfg, ok := sshPOSIXConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_POSIX_HOST / _USER / _PASS not set")
	}

	pluginPath := buildPluginForSSHTest(t, filepath.Join("..", "plugins", "testdata", "pluginhandle"))
	reg := target.ModuleRegistry{
		"testhandle": plugins.NewModule("testhandle", pluginPath),
	}
	tgt := target.NewSSHTarget(cfg, reg)
	t.Cleanup(func() { _ = tgt.Close() })

	assertPOSIXSentinel(t, tgt)

	t.Run("all_ops_round_trip", func(t *testing.T) {
		putPath := fmt.Sprintf("/tmp/pf-plugin-put-%d", time.Now().UnixNano())
		// The plugin self-verifies RunCommand, PutFile, GetFile, and Info, and
		// returns the ops-ok marker only when all four passed. A failure in
		// any op surfaces as a mismatch message instead.
		res, err := tgt.Execute(context.Background(), "task-1", "testhandle",
			map[string]any{"scenario": "ops", "put_path": putPath},
			target.ExecutionOptions{}, true, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.HasPrefix(res.Message, "ops-ok:") {
			t.Fatalf("Check message = %q, want ops-ok: prefix", res.Message)
		}
		if res.Status != target.StatusChanged {
			t.Errorf("status = %q, want changed", res.Status)
		}
		// Independent oracle: PutFile wrote through the SSH handle backend.
		got, err := tgt.ReadFile(context.Background(), putPath)
		if err != nil {
			t.Fatalf("oracle read %q: %v", putPath, err)
		}
		if strings.TrimSpace(string(got)) != "plugin-put-bytes" {
			t.Errorf("put file content = %q, want %q", got, "plugin-put-bytes")
		}
	})

	t.Run("streaming_output", func(t *testing.T) {
		var lines []string
		_, err := tgt.Execute(context.Background(), "task-2", "testhandle",
			map[string]any{"scenario": "streaming"}, target.ExecutionOptions{}, true,
			func(line string) { lines = append(lines, line) })
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		want := []string{"stream-1", "stream-2"}
		if len(lines) != len(want) {
			t.Fatalf("output lines = %v, want %v", lines, want)
		}
		for i, w := range want {
			if lines[i] != w {
				t.Errorf("line %d = %q, want %q", i, lines[i], w)
			}
		}
	})

	t.Run("become_refused", func(t *testing.T) {
		_, err := tgt.Execute(context.Background(), "task-3", "testhandle",
			map[string]any{"scenario": "ops"}, target.ExecutionOptions{
				Become: &target.BecomeOptions{Enabled: true, User: "root"},
			}, false, nil)
		if err == nil {
			t.Fatal("expected plugin+become to be refused, got nil")
		}
		var mse *target.ModuleSupportError
		if !errors.As(err, &mse) {
			t.Fatalf("expected *ModuleSupportError, got %T: %v", err, err)
		}
		if mse.Class != target.ClassPluginBecome {
			t.Errorf("class = %q, want %q", mse.Class, target.ClassPluginBecome)
		}
		if mse.ReasonCode() != "plugin_become" {
			t.Errorf("reason = %q, want plugin_become", mse.ReasonCode())
		}
	})

	t.Run("protocol_version_rejection", func(t *testing.T) {
		prev1Path := buildPluginForSSHTest(t, filepath.Join("..", "plugins", "testdata", "prev1plugin"))
		prev1Reg := target.ModuleRegistry{
			"prev1": plugins.NewModule("prev1", prev1Path),
		}
		prev1Tgt := target.NewSSHTarget(cfg, prev1Reg)
		t.Cleanup(func() { _ = prev1Tgt.Close() })

		_, err := prev1Tgt.Execute(context.Background(), "task-4", "prev1",
			map[string]any{}, target.ExecutionOptions{}, false, nil)
		if err == nil {
			t.Fatal("expected protocol error for pre-v1 plugin, got nil")
		}
		var mse *target.ModuleSupportError
		if !errors.As(err, &mse) || mse.Class != target.ClassPluginProtocol {
			t.Fatalf("expected plugin_protocol class, got %T: %v", err, err)
		}
		if mse.ReasonCode() != "plugin_protocol" {
			t.Errorf("reason = %q, want plugin_protocol", mse.ReasonCode())
		}
	})
}

// sshPOSIXConfigFromEnv mirrors target.getSSHPOSIXConfigFromEnv using only
// exported types, so this external test package can build an SSH target
// without depending on the unexported harness.
func sshPOSIXConfigFromEnv() (target.SSHConfig, bool) {
	host := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_HOST")
	user := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_USER")
	pass := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_PASS")
	if host == "" || user == "" || pass == "" {
		return target.SSHConfig{}, false
	}
	port := 22
	if raw := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_PORT"); raw != "" {
		if p, err := atoi(raw); err == nil && p > 0 {
			port = p
		}
	}
	return target.SSHConfig{
		Host:          host,
		Port:          port,
		Username:      user,
		Password:      pass,
		HostKeyPolicy: target.HostKeyPolicyInsecure,
	}, true
}

// assertPOSIXSentinel skips the test when the sacrificial sentinel is absent
// on the target, mirroring target.assertPOSIXSacrificialSentinel via the
// exported ReadFile oracle.
func assertPOSIXSentinel(t *testing.T, tgt *target.SSHTarget) {
	t.Helper()
	if _, err := tgt.ReadFile(context.Background(), "/etc/preflight-test-sacrificial"); err != nil {
		t.Skipf("sacrificial sentinel not found on target: %v. Ensure /etc/preflight-test-sacrificial exists (see test/posix/README.md).", err)
	}
}

func atoi(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// buildPluginForSSHTest builds a controller-side test plugin binary from the
// given source directory (relative to this test file's package). It is the
// external-test counterpart of plugins_test.buildPlugin so this suite can
// drive the same testdata binaries over SSH.
func buildPluginForSSHTest(t *testing.T, srcDir string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping plugin subprocess build in -short mode")
	}
	name := filepath.Base(srcDir)
	out := filepath.Join(t.TempDir(), pluginExeName("preflight-plugin-"+name))
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build test plugin %s: %v\n%s", srcDir, err, buildOut)
	}
	return out
}

func pluginExeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
