//go:build integration

package target

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_POSIXBecomePostureMatrix exercises the three-user privilege
// posture matrix from spec §5/§8 over a real POSIX SSH target. It connects as
// each provisioned user (pf-admin NOPASSWD, pf-sudopass password-sudo,
// pf-nosudo no sudo) and asserts the become mechanics:
//
//   - NOPASSWD path: become to root with no password runs a shell task as root.
//   - password path: become to root with the sudo password runs a shell task
//     as root (sudo -S stdin mechanics; the secret:-backed become.password
//     plumbing is covered at the runner layer by TestApplyResolvesSecrets).
//   - no-sudo refusal: become to root on a user with no sudoers entry fails
//     deterministically with the sudo-password-required reason code (sudo -n
//     fail-fast), not a hang.
//
// The requires-root-violation path is unit-tested in become_env_error_test.go;
// its integration coverage lands with the POSIX service/user/system_package
// modules (sibling task). sudo-missing is unit-tested only (no target lacks
// sudo in the matrix).
//
// The host/port come from PREFLIGHT_TEST_SSH_POSIX_*; the three users are
// fixed by the container Dockerfiles (password "preflight" for all).
func TestIntegration_POSIXBecomePostureMatrix(t *testing.T) {
	cfg, ok := getSSHPOSIXConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_POSIX_HOST / _USER / _PASS not set")
	}

	// The three posture users provisioned by the container. Each connects
	// with password "preflight".
	postureUsers := []struct {
		name string
		user string
	}{
		{"NOPASSWD", "pf-admin"},
		{"password-sudo", "pf-sudopass"},
		{"no-sudo", "pf-nosudo"},
	}
	const posturePass = "preflight"

	for _, pu := range postureUsers {
		t.Run(pu.name, func(t *testing.T) {
			userCfg := *cfg
			userCfg.Username = pu.user
			userCfg.Password = posturePass
			tgt := NewSSHTarget(userCfg, nil)
			t.Cleanup(func() { _ = tgt.Close() })
			assertPOSIXSacrificialSentinel(t, tgt)

			switch pu.user {
			case "pf-admin":
				// NOPASSWD path: become to root, no password. shell task runs as root.
				result, err := tgt.Execute(context.Background(), "become-nopasswd", "shell", map[string]any{
					"cmd": "sh",
					"args": []string{
						"-c",
						"id -u > " + fmt.Sprintf("/tmp/pf-become-id-%s", testRunID()[:12]),
					},
				}, ExecutionOptions{Become: &BecomeOptions{Enabled: true}}, false, nil)
				if err != nil {
					t.Fatalf("NOPASSWD become execute: %v", err)
				}
				if result.Status != StatusChanged {
					t.Fatalf("NOPASSWD become status: got %q, want %q", result.Status, StatusChanged)
				}
				// Oracle: the task ran as root (euid 0).
				uid := strings.TrimSpace(posixRun(t, tgt, fmt.Sprintf("cat /tmp/pf-become-id-%s 2>/dev/null || echo missing", testRunID()[:12])))
				if uid != "0" {
					t.Fatalf("NOPASSWD become did not run as root: identity=%q", uid)
				}
				_, _, _, _ = tgt.run(context.Background(), fmt.Sprintf("rm -f /tmp/pf-become-id-%s", testRunID()[:12]), nil)

			case "pf-sudopass":
				// Password path: become to root with the sudo password (sudo -S).
				result, err := tgt.Execute(context.Background(), "become-password", "shell", map[string]any{
					"cmd": "sh",
					"args": []string{
						"-c",
						idToFileCmd(testRunID()[:12]),
					},
				}, ExecutionOptions{Become: &BecomeOptions{Enabled: true, Password: posturePass}}, false, nil)
				if err != nil {
					t.Fatalf("password become execute: %v", err)
				}
				if result.Status != StatusChanged {
					t.Fatalf("password become status: got %q, want %q", result.Status, StatusChanged)
				}
				uid := strings.TrimSpace(posixRun(t, tgt, fmt.Sprintf("cat /tmp/pf-become-id-%s 2>/dev/null || echo missing", testRunID()[:12])))
				if uid != "0" {
					t.Fatalf("password become did not run as root: identity=%q", uid)
				}
				_, _, _, _ = tgt.run(context.Background(), fmt.Sprintf("rm -f /tmp/pf-become-id-%s", testRunID()[:12]), nil)

			case "pf-nosudo":
				// No-sudo refusal: become to root with no password. sudo -n
				// fails deterministically with sudo-password-required (the
				// user has no sudoers entry, so sudo wants a password).
				_, err := tgt.Execute(context.Background(), "become-nosudo", "shell", map[string]any{
					"cmd":  "sh",
					"args": []string{"-c", "true"},
				}, ExecutionOptions{Become: &BecomeOptions{Enabled: true}}, false, nil)
				if err == nil {
					t.Fatal("no-sudo become: expected error, got nil")
				}
				if got := ReasonCodeForError(err); got != "sudo-password-required" {
					t.Fatalf("no-sudo become reason: got %q, want sudo-password-required (err=%v)", got, err)
				}
				var be *BecomeEnvError
				if !errors.As(err, &be) {
					t.Fatalf("expected *BecomeEnvError, got %T: %v", err, err)
				}
			}
		})
	}
}

// idToFileCmd writes `id -u` to a per-run temp file for the become identity
// oracle.
func idToFileCmd(suffix string) string {
	return "id -u > /tmp/pf-become-id-" + suffix
}
