//go:build integration

package target

import (
	"context"
	"strings"
	"testing"
)

// TestIntegration_POSIXService exercises the service module over SSH against a
// real systemd-enabled POSIX target. It connects as the (non-root) SSH user
// and escalates to root via become — exercising the requires_root enforcement
// path — then drives state and startup_type transitions against a sacrificial
// one-shot unit, verifying convergence with an independent systemctl oracle.
//
// Coverage (mirrors the Windows service suite's shape):
//
//   - state running/stopped: start, idempotent, stop, idempotent
//   - startup_type automatic/manual/disabled: enable, disable, mask, each idempotent
//   - state disabled: stop + mask in one step, idempotent
//
// Runs in both CI containers (Ubuntu + Rocky) — the harness is invoked per
// container by the CI runner, so this single function covers the matrix.
func TestIntegration_POSIXService(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		svc := "preflight-test-svc-" + testRunID()[:12]
		// The unit file lives in the vendor path so `systemctl mask` can shadow it
		// with an /etc/systemd/system/<svc>.service -> /dev/null symlink. Placing
		// the real unit file in /etc/systemd/system/ makes mask fail with "File
		// already exists" — masking is designed to shadow vendor units.
		unitPath := "/usr/lib/systemd/system/" + svc + ".service"
		// become to root: service is requires_root and the SSH user is unprivileged.
		become := ExecutionOptions{Become: &BecomeOptions{Enabled: true}}

		unitContent := "[Unit]\nDescription=Preflight test service\n" +
			"[Service]\nType=simple\nExecStart=/bin/sleep 100000\n" +
			"[Install]\nWantedBy=multi-user.target\n"

		// setup: stage the unit file (as root via become) and reload systemd.
		mustExecute(t, tgt, "svc-create-unit", "file", map[string]any{
			"dest":    unitPath,
			"content": unitContent,
		}, become, false, StatusChanged)
		mustExecute(t, tgt, "svc-daemon-reload", "shell", map[string]any{
			"cmd":  "systemctl",
			"args": []string{"daemon-reload"},
		}, become, false, StatusChanged)

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, "sudo systemctl stop "+svc+" 2>/dev/null; "+
				"sudo systemctl unmask "+svc+" 2>/dev/null; "+
				"sudo rm -f "+unitPath+"; sudo systemctl daemon-reload", nil)
		})

		// Oracle: capture systemctl state as root via sudo. pf-admin has NOPASSWD
		// sudo, and a non-root SSH session cannot reach systemd's D-Bus in these
		// containers. is-active exits non-zero for inactive units and is-enabled
		// exits non-zero for disabled/masked units, but both print the state to
		// stdout — so capture stdout without asserting exit code.
		isActive := func() string {
			stdout, _, _, _ := tgt.run(ctx, "sudo systemctl is-active "+svc, nil)
			return strings.TrimSpace(stdout)
		}
		isEnabled := func() string {
			stdout, _, _, _ := tgt.run(ctx, "sudo systemctl is-enabled "+svc, nil)
			return strings.TrimSpace(stdout)
		}

		// ================================================================
		// state: running -> active
		// ================================================================
		mustExecute(t, tgt, "svc-start", "service", map[string]any{
			"name": svc, "state": "running",
		}, become, false, StatusChanged)
		if got := isActive(); got != "active" {
			t.Fatalf("state=running: is-active=%q, want active", got)
		}
		mustExecute(t, tgt, "svc-start-idemp", "service", map[string]any{
			"name": svc, "state": "running",
		}, become, false, StatusOK)

		// ================================================================
		// state: stopped -> inactive
		// ================================================================
		mustExecute(t, tgt, "svc-stop", "service", map[string]any{
			"name": svc, "state": "stopped",
		}, become, false, StatusChanged)
		if got := isActive(); got != "inactive" {
			t.Fatalf("state=stopped: is-active=%q, want inactive", got)
		}
		mustExecute(t, tgt, "svc-stop-idemp", "service", map[string]any{
			"name": svc, "state": "stopped",
		}, become, false, StatusOK)

		// ================================================================
		// startup_type: automatic -> enabled
		// ================================================================
		mustExecute(t, tgt, "svc-enable", "service", map[string]any{
			"name": svc, "startup_type": "automatic",
		}, become, false, StatusChanged)
		if got := isEnabled(); got != "enabled" {
			t.Fatalf("startup_type=automatic: is-enabled=%q, want enabled", got)
		}
		mustExecute(t, tgt, "svc-enable-idemp", "service", map[string]any{
			"name": svc, "startup_type": "automatic",
		}, become, false, StatusOK)

		// ================================================================
		// startup_type: manual -> disabled
		// ================================================================
		mustExecute(t, tgt, "svc-disable", "service", map[string]any{
			"name": svc, "startup_type": "manual",
		}, become, false, StatusChanged)
		if got := isEnabled(); got != "disabled" {
			t.Fatalf("startup_type=manual: is-enabled=%q, want disabled", got)
		}
		mustExecute(t, tgt, "svc-disable-idemp", "service", map[string]any{
			"name": svc, "startup_type": "manual",
		}, become, false, StatusOK)

		// ================================================================
		// startup_type: disabled -> masked
		// ================================================================
		mustExecute(t, tgt, "svc-mask", "service", map[string]any{
			"name": svc, "startup_type": "disabled",
		}, become, false, StatusChanged)
		if got := isEnabled(); got != "masked" {
			t.Fatalf("startup_type=disabled: is-enabled=%q, want masked", got)
		}
		mustExecute(t, tgt, "svc-mask-idemp", "service", map[string]any{
			"name": svc, "startup_type": "disabled",
		}, become, false, StatusOK)

		// Unmask so the unit is startable for the state=disabled step.
		_, _, _, _ = tgt.run(ctx, "sudo systemctl unmask "+svc, nil)

		// ================================================================
		// state: disabled -> stop + mask (short-circuit)
		// ================================================================
		// Start first so the stop half of state=disabled is observable.
		mustExecute(t, tgt, "svc-start-for-disabled", "service", map[string]any{
			"name": svc, "state": "running",
		}, become, false, StatusChanged)
		mustExecute(t, tgt, "svc-disabled", "service", map[string]any{
			"name": svc, "state": "disabled",
		}, become, false, StatusChanged)
		if got := isActive(); got != "inactive" {
			t.Fatalf("state=disabled: is-active=%q, want inactive", got)
		}
		if got := isEnabled(); got != "masked" {
			t.Fatalf("state=disabled: is-enabled=%q, want masked", got)
		}
		mustExecute(t, tgt, "svc-disabled-idemp", "service", map[string]any{
			"name": svc, "state": "disabled",
		}, become, false, StatusOK)
	})
}
