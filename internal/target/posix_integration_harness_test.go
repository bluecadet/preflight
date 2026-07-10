//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// getSSHPOSIXConfigFromEnv builds the SSH-to-POSIX connection config from
// the PREFLIGHT_TEST_SSH_POSIX_* env vars. Returns nil + false when any
// required var is missing so callers can t.Skip cleanly. Shares one
// implementation with the SSH-to-Windows vars via sshConfigFromEnv.
func getSSHPOSIXConfigFromEnv() (*SSHConfig, bool) {
	return sshConfigFromEnv("PREFLIGHT_TEST_SSH_POSIX")
}

// assertPOSIXSacrificialSentinel checks that the target has the sacrificial
// sentinel marker at /etc/preflight-test-sacrificial. Without this marker
// the test refuses to mutate the target, preventing accidental changes to a
// non-sacrificial machine.
func assertPOSIXSacrificialSentinel(t *testing.T, tgt *SSHTarget) {
	t.Helper()
	ctx := context.Background()
	stdout, _, _, err := tgt.run(ctx, "test -f /etc/preflight-test-sacrificial && echo present || echo absent", nil)
	if err != nil {
		t.Fatalf("sentinel check failed: %v — cannot proceed", err)
	}
	if strings.TrimSpace(stdout) != "present" {
		t.Skipf("sacrificial sentinel not found on target (response: %q). "+
			"Ensure /etc/preflight-test-sacrificial exists on the target "+
			"(see test/posix/README.md).", strings.TrimSpace(stdout))
	}
}

// forEachPOSIXTarget builds an SSHTarget from the PREFLIGHT_TEST_SSH_POSIX_*
// env vars, asserts the sacrificial sentinel, and runs fn. Skips cleanly when
// the env vars are unset.
//
// The fn callback receives a *SSHTarget, which implements the Target
// interface for Execute calls and also exposes the unexported run method for
// independent oracle commands (see posixRun).
func forEachPOSIXTarget(t *testing.T, fn func(t *testing.T, tgt *SSHTarget)) {
	t.Helper()
	cfg, ok := getSSHPOSIXConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_POSIX_HOST / _USER / _PASS not set")
	}
	tgt := NewSSHTarget(*cfg, nil)
	t.Cleanup(func() { _ = tgt.Close() })
	assertPOSIXSacrificialSentinel(t, tgt)
	fn(t, tgt)
}

// posixRun is an oracle helper that runs a shell command on the target and
// returns its stdout. It fails the test on any SSH-level error or non-zero
// exit code. It is an independent verification path — separate from module
// Check/Apply — used to assert the actual state of the target.
func posixRun(t *testing.T, tgt *SSHTarget, cmd string) string {
	t.Helper()
	ctx := context.Background()
	stdout, stderr, code, err := tgt.run(ctx, cmd, nil)
	if err != nil {
		t.Fatalf("oracle command failed: %v\nstderr: %s", err, stderr)
	}
	if code != 0 {
		t.Fatalf("oracle command exited %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout
}

// posixExitCode runs a shell command and returns its exit code. It fails the
// test on SSH-level errors but returns the exit code as-is for command-level
// failures (e.g. test -e returning 1). Used for existence checks where
// non-zero exit means "not found" rather than "error".
func posixExitCode(t *testing.T, tgt *SSHTarget, cmd string) int {
	t.Helper()
	ctx := context.Background()
	_, _, code, err := tgt.run(ctx, cmd, nil)
	if err != nil {
		t.Fatalf("oracle command failed: %v", err)
	}
	return code
}

// posixRemoteFile reads a file on the target via the oracle path (plain
// cat) and returns its contents. This is independent of the file module's
// CopyFile/ReadFile implementation, which uses base64 transport.
func posixRemoteFile(t *testing.T, tgt *SSHTarget, path string) string {
	t.Helper()
	return posixRun(t, tgt, fmt.Sprintf("cat %q", path))
}
