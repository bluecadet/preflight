//go:build integration

package target

import (
	"fmt"
	"testing"
	"time"
)

// TestIntegration_POSIXOpsConformance runs the shared ops-interface
// conformance suite against a real SSH-POSIX target (the CI containers).
// Matches the CI -run TestIntegration_POSIX filter so it runs on every PR.
func TestIntegration_POSIXOpsConformance(t *testing.T) {
	cfg, ok := getSSHPOSIXConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_POSIX_HOST / _USER / _PASS not set")
	}
	tgt := NewSSHTarget(*cfg, nil)
	t.Cleanup(func() { _ = tgt.Close() })
	assertPOSIXSacrificialSentinel(t, tgt)
	runOpsConformance(t, tgt, func() string {
		return fmt.Sprintf("/tmp/pf-ops-conf-%s-%d", testRunID()[:12], time.Now().UnixNano())
	})
}

// TestIntegration_SSHWindowsOpsConformance runs the shared ops-interface
// conformance suite against an SSH-Windows target. Dev-only: requires the
// PREFLIGHT_TEST_SSH_* Windows VM env vars and skips cleanly otherwise.
func TestIntegration_SSHWindowsOpsConformance(t *testing.T) {
	cfg, ok := getSSHConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_HOST / _USER / _PASS not set")
	}
	tgt := NewSSHTarget(*cfg, nil)
	t.Cleanup(func() { _ = tgt.Close() })
	assertSacrificialSentinel(t, tgt)
	runOpsConformance(t, tgt, func() string {
		return fmt.Sprintf(`%s\pf-ops-conf-%s-%d`, windowsRemoteTempDir, testRunID()[:12], time.Now().UnixNano())
	})
}

// TestIntegration_WinRMOpsConformance runs the shared ops-interface conformance
// suite against a WinRM target. Dev-only: requires the
// PREFLIGHT_TEST_WINRM_* env vars and skips cleanly otherwise.
func TestIntegration_WinRMOpsConformance(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS not set")
	}
	tgt := NewWinRMTarget(*cfg, nil)
	t.Cleanup(func() { _ = tgt.Close() })
	assertSacrificialSentinel(t, tgt)
	runOpsConformance(t, tgt, func() string {
		return fmt.Sprintf(`%s\pf-ops-conf-%s-%d`, windowsRemoteTempDir, testRunID()[:12], time.Now().UnixNano())
	})
}
