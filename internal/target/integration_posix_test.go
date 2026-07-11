//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestIntegration_POSIXFile exercises the file module over SSH against a real
// POSIX target. Coverage mirrors the Windows registry test:
//
//   - present:     create a file with content, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift:       mutate content behind the module's back, verify convergence
//   - absent:      remove the file, idempotent re-check
func TestIntegration_POSIXFile(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		dest := fmt.Sprintf("/tmp/preflight-test-file-%s", testRunID()[:12])

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("rm -f %q", dest), nil)
		})

		content := "hello preflight\n"
		params := map[string]any{
			"dest":    dest,
			"content": content,
		}

		// ---- present: create file with content ----
		mustExecute(t, tgt, "file-present", "file", params, ExecutionOptions{}, false, StatusChanged)
		if got := posixRemoteFile(t, tgt, dest); got != content {
			t.Fatalf("file content: got %q, want %q", got, content)
		}

		// ---- idempotent: re-check and re-apply both return OK ----
		mustExecute(t, tgt, "file-idemp-check", "file", params, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "file-idemp-apply", "file", params, ExecutionOptions{}, false, StatusOK)

		// ---- dry-run: predicts Changed, no mutation ----
		dryParams := map[string]any{
			"dest":    dest,
			"content": "different content\n",
		}
		mustExecute(t, tgt, "file-dryrun", "file", dryParams, ExecutionOptions{}, true, StatusChanged)
		if got := posixRemoteFile(t, tgt, dest); got != content {
			t.Fatalf("dry-run mutated file: got %q, want %q", got, content)
		}

		// ---- drift: change behind the module, then converge ----
		posixRun(t, tgt, fmt.Sprintf("echo 'drifted' > %q", dest))
		mustExecute(t, tgt, "file-drift-check", "file", params, ExecutionOptions{}, true, StatusChanged)
		mustExecute(t, tgt, "file-drift-apply", "file", params, ExecutionOptions{}, false, StatusChanged)
		if got := posixRemoteFile(t, tgt, dest); got != content {
			t.Fatalf("drift convergence: got %q, want %q", got, content)
		}

		// ---- absent: remove the file ----
		absentParams := map[string]any{
			"dest":   dest,
			"ensure": "absent",
		}
		mustExecute(t, tgt, "file-absent", "file", absentParams, ExecutionOptions{}, false, StatusChanged)
		if code := posixExitCode(t, tgt, fmt.Sprintf("test -e %q", dest)); code == 0 {
			t.Fatalf("file still exists after absent")
		}

		// ---- idempotent absent ----
		mustExecute(t, tgt, "file-absent-idemp", "file", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// TestIntegration_POSIXDirectory exercises the directory module over SSH
// against a real POSIX target:
//
//   - present:     create a directory, verify via oracle
//   - idempotent:  re-check returns OK
//   - absent:      remove, verify, idempotent re-check
func TestIntegration_POSIXDirectory(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		path := fmt.Sprintf("/tmp/preflight-test-dir-%s", testRunID()[:12])

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("rm -rf %q", path), nil)
		})

		// ---- present: create directory ----
		params := map[string]any{"path": path}
		mustExecute(t, tgt, "dir-present", "directory", params, ExecutionOptions{}, false, StatusChanged)
		if code := posixExitCode(t, tgt, fmt.Sprintf("test -d %q", path)); code != 0 {
			t.Fatalf("directory was not created: test -d %q failed", path)
		}

		// ---- idempotent: re-check returns OK ----
		mustExecute(t, tgt, "dir-idemp-check", "directory", params, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "dir-idemp-apply", "directory", params, ExecutionOptions{}, false, StatusOK)

		// ---- absent: remove directory ----
		absentParams := map[string]any{"path": path, "ensure": "absent"}
		mustExecute(t, tgt, "dir-absent", "directory", absentParams, ExecutionOptions{}, false, StatusChanged)
		if code := posixExitCode(t, tgt, fmt.Sprintf("test -e %q", path)); code == 0 {
			t.Fatalf("directory still exists after absent")
		}

		// ---- idempotent absent ----
		mustExecute(t, tgt, "dir-absent-idemp", "directory", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// TestIntegration_POSIXShell exercises the shell module over SSH against a
// real POSIX target:
//
//   - creates guard: first run creates a file, verify via oracle
//   - idempotent:    re-check returns OK (creates guard satisfied)
//   - streaming:     multi-line command output is captured in Result.Output
func TestIntegration_POSIXShell(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		createsFile := fmt.Sprintf("/tmp/preflight-test-shell-%s", testRunID()[:12])

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("rm -f %q", createsFile), nil)
		})

		// ---- creates guard: first run creates the file ----
		// The shell module runs cmd+args as a quoted argv, not a shell expression.
		// Use sh -c so redirects and pipelines are interpreted.
		params := map[string]any{
			"cmd":     "sh",
			"args":    []string{"-c", "echo shell-test > " + createsFile},
			"creates": createsFile,
		}
		mustExecute(t, tgt, "shell-run", "shell", params, ExecutionOptions{}, false, StatusChanged)
		got := strings.TrimSpace(posixRemoteFile(t, tgt, createsFile))
		if got != "shell-test" {
			t.Fatalf("shell output: got %q, want %q", got, "shell-test")
		}

		// ---- creates guard: re-check returns OK (file exists) ----
		mustExecute(t, tgt, "shell-idemp-check", "shell", params, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "shell-idemp-apply", "shell", params, ExecutionOptions{}, false, StatusOK)

		// ---- streaming: multi-line output captured in Result.Output ----
		streamResult := mustExecute(t, tgt, "shell-streaming", "shell", map[string]any{
			"cmd":  "sh",
			"args": []string{"-c", "echo line1; echo line2; echo line3"},
		}, ExecutionOptions{}, false, StatusChanged)

		want := []string{"line1", "line2", "line3"}
		if len(streamResult.Output) < len(want) {
			t.Fatalf("expected at least %d output lines, got %d: %v", len(want), len(streamResult.Output), streamResult.Output)
		}
		for i, w := range want {
			if streamResult.Output[i] != w {
				t.Fatalf("output line %d: got %q, want %q", i, streamResult.Output[i], w)
			}
		}
	})
}

// TestIntegration_POSIXWait exercises the wait module over SSH against a real
// POSIX target. Both file_exists and port_open conditions are tested in two
// modes:
//
//   - already met: condition is satisfied at check time → StatusOK
//   - eventually met: condition becomes true during the apply poll → StatusChanged
//
// The "eventually met" path schedules a background process (via nohup) that
// creates a file or starts a listener after a short delay. The wait module
// polls every 5 seconds; the delay is set to 3s so the first poll fails and
// the second succeeds, exercising the polling loop.
func TestIntegration_POSIXWait(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		filePath := fmt.Sprintf("/tmp/preflight-test-wait-%s", testRunID()[:12])

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("rm -f %q", filePath), nil)
			_, _, _, _ = tgt.run(ctx, "pkill -f 'nc -l 889' 2>/dev/null; true", nil)
		})

		// ================================================================
		// file_exists
		// ================================================================

		// ---- already met: file exists → StatusOK ----
		posixRun(t, tgt, fmt.Sprintf("touch %q", filePath))
		mustExecute(t, tgt, "wait-file-exists", "wait", map[string]any{
			"condition": "file_exists",
			"target":    filePath,
			"timeout":   "15s",
		}, ExecutionOptions{}, false, StatusOK)

		// ---- eventually met: file appears after delay → StatusChanged ----
		posixRun(t, tgt, fmt.Sprintf("rm -f %q", filePath))
		posixRun(t, tgt, fmt.Sprintf("nohup sh -c 'sleep 3; touch %q' >/dev/null 2>&1 &", filePath))
		mustExecute(t, tgt, "wait-file-create", "wait", map[string]any{
			"condition": "file_exists",
			"target":    filePath,
			"timeout":   "30s",
		}, ExecutionOptions{}, false, StatusChanged)

		// ================================================================
		// port_open
		// ================================================================

		// ---- already met: listener running → StatusOK ----
		// Start a listener in the background. nc -l exits after one connection,
		// so sleep gives it time to bind before the wait module's Check probes.
		// The Check probe connects and succeeds (port was open), returning OK.
		posixRun(t, tgt, "nohup nc -l 8890 >/dev/null 2>&1 & sleep 2")
		mustExecute(t, tgt, "wait-port-open", "wait", map[string]any{
			"condition": "port_open",
			"target":    "localhost:8890",
			"timeout":   "15s",
		}, ExecutionOptions{}, false, StatusOK)

		// ---- eventually met: listener starts after delay → StatusChanged ----
		posixRun(t, tgt, "nohup sh -c 'sleep 3; nc -l 8891' >/dev/null 2>&1 &")
		mustExecute(t, tgt, "wait-port-create", "wait", map[string]any{
			"condition": "port_open",
			"target":    "localhost:8891",
			"timeout":   "30s",
		}, ExecutionOptions{}, false, StatusChanged)
	})
}

// TestIntegration_POSIXWaitServiceRunning exercises the wait module's
// service_running condition over SSH against a real POSIX target with systemd.
// It starts a transient system service (via sudo), waits for it to reach
// active, and asserts the condition reports met at check time (StatusOK); then
// stops the service and asserts the wait times out with StatusFailed within a
// short deadline.
func TestIntegration_POSIXWaitServiceRunning(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		svc := fmt.Sprintf("preflight-test-svc-%s.service", testRunID()[:8])

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo systemctl stop %q 2>/dev/null; sudo systemctl reset-failed %q 2>/dev/null; true", svc, svc), nil)
		})

		// Start a transient system service that sleeps long enough to stay active.
		posixRun(t, tgt, fmt.Sprintf(
			"sudo systemd-run --unit=%q --service-type=exec /bin/sh -c 'sleep 300'", svc))
		// Wait for the unit to reach active before asserting.
		posixRun(t, tgt, fmt.Sprintf(
			"for i in 1 2 3 4 5; do sudo systemctl is-active --quiet %q && exit 0; sleep 1; done", svc))

		// Condition met at check time → StatusOK.
		mustExecute(t, tgt, "wait-svc-active", "wait", map[string]any{
			"condition": "service_running",
			"target":    svc,
			"timeout":   "15s",
		}, ExecutionOptions{}, false, StatusOK)

		// Stop the service; the condition is no longer met. Apply polls until the
		// short timeout expires → StatusFailed.
		posixRun(t, tgt, fmt.Sprintf("sudo systemctl stop %q", svc))
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		res, err := tgt.Execute(ctx, "wait-svc-stopped", "wait", map[string]any{
			"condition": "service_running",
			"target":    svc,
			"timeout":   "6s",
		}, ExecutionOptions{}, false, nil)
		if err == nil && res.Status != StatusFailed {
			t.Fatalf("expected StatusFailed for a stopped service within timeout, got %q: %s", res.Status, res.Message)
		}
	})
}

// TestIntegration_POSIXRebootIfNeeded exercises the reboot module's if_needed
// condition over SSH against a real POSIX target. The probe is driven by
// planting/removing the /var/run/reboot-required marker signal (honored on both
// apt and dnf hosts), so the test runs in both CI containers:
//
//   - marker absent  → no reboot needed (StatusOK)
//   - marker planted → reboot needed (StatusChanged on a dry-run, which asserts
//     detection without actually rebooting the target)
//
// reboot requires root, so the task runs with become enabled (pf-admin has
// NOPASSWD sudo). The real reboot+reconnect path is not exercised here (stated
// limitation); only the if_needed probe detection is covered.
func TestIntegration_POSIXRebootIfNeeded(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		marker := "/var/run/reboot-required"

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo rm -f %q", marker), nil)
		})

		// Ensure the marker is absent: if_needed → no reboot needed.
		posixRun(t, tgt, fmt.Sprintf("sudo rm -f %q", marker))
		mustExecute(t, tgt, "reboot-none", "reboot", map[string]any{
			"condition": "if_needed",
		}, ExecutionOptions{Become: &BecomeOptions{Enabled: true}}, false, StatusOK)

		// Plant the marker: if_needed → reboot needed. Use dry-run so the change
		// is detected (StatusChanged) without actually rebooting the target.
		posixRun(t, tgt, fmt.Sprintf("sudo touch %q", marker))
		mustExecute(t, tgt, "reboot-detected", "reboot", map[string]any{
			"condition": "if_needed",
		}, ExecutionOptions{Become: &BecomeOptions{Enabled: true}}, true, StatusChanged)
	})
}
