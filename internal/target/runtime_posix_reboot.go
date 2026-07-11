package target

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// --- reboot ----------------------------------------------------------------

// checkPOSIXReboot decides whether a reboot is needed. condition "always"
// always reports a needed change. condition "if_needed" (the default) probes
// the distro-appropriate reboot-required signal, driven by the cached package
// manager detection: apt → /var/run/reboot-required marker file; dnf →
// `needs-restarting -r` (exit 1 = reboot required). When neither package
// manager is detected, no reboot signal is available and Check reports no
// change with a message stating the situation. Both conditions require
// systemd (the reboot path uses systemctl); an empty init signal fails
// per-task with the typed environment-prerequisite error.
func checkPOSIXReboot(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	condition, _ := params["condition"].(string)
	if condition == "" {
		condition = "if_needed"
	}

	probe, err := backend.Probe(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	if probe.Init != "systemd" {
		return CheckResult{}, NewMissingPrerequisiteError("reboot", RuntimeKindPOSIXShell,
			"requires systemd; no init system detected on the target")
	}

	if condition == "always" {
		return CheckResult{NeedsChange: true}, nil
	}

	needed, message, err := posixRebootPending(ctx, backend, probe.PackageManager)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: needed, Message: message}, nil
}

// posixRebootPending probes the distro reboot-required signals and returns
// whether a reboot is needed plus an optional status message (used when no
// signal is available). Two signals are probed, matching the spec:
//
//   - /var/run/reboot-required — the apt/unattended-upgrades convention. It is
//     checked on every distro because it is a plantable marker (used by the
//     integration suite) and because some tooling creates it on non-apt hosts.
//   - needs-restarting -r — the dnf/RHEL signal, gated to dnf systems. It exits
//     1 when a reboot is required, 0 otherwise, and 127 when the binary is
//     absent (treated as no signal available).
//
// The cached package-manager detection drives the interpretation: on apt the
// marker file is the signal (its absence means no reboot); on dnf
// needs-restarting is the signal (and the file is a bonus plantable marker);
// with no supported package manager, no signal is available.
func posixRebootPending(ctx context.Context, backend posixShellBackend, pkgManager string) (needed bool, message string, err error) {
	// 1. Marker file — checked first on every distro.
	_, _, code, err := backend.RunPOSIXCommand(ctx, "test -f /var/run/reboot-required", nil)
	if err != nil {
		return false, "", err
	}
	if code == 0 {
		return true, "", nil
	}

	// 2. dnf needs-restarting -r — the dnf/RHEL signal.
	if pkgManager == "dnf" {
		_, stderr, nrCode, err := backend.RunPOSIXCommand(ctx, "needs-restarting -r", nil)
		if err != nil {
			return false, "", err
		}
		switch nrCode {
		case 1:
			return true, "", nil
		case 0:
			return false, "", nil // signal available, says no reboot
		}
		// 127 or any other code: needs-restarting is unavailable → no signal.
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = "needs-restarting unavailable"
		}
		return false, "no reboot-required signal available; no reboot needed (" + detail + ")", nil
	}

	// 3. apt with an absent marker: the file is the signal and it says no reboot.
	if pkgManager == "apt" {
		return false, "", nil
	}

	// 4. No supported package manager: neither signal is available. Treat as
	//    no reboot needed and state so in the output.
	return false, "no reboot-required signal available; no reboot needed (no supported package manager detected)", nil
}

// applyPOSIXReboot issues `systemctl reboot` and then waits for the target to
// come back, polling a lightweight command until it reconnects within the
// timeout. The reboot command itself is expected to drop the connection; its
// error is ignored. The reconnect relies on the transport's one-shot
// reconnect-and-retry: each poll attempt re-dials on a dead connection.
//
// The real reboot+reconnect path is unit-tested against fakes only and is a
// stated limitation — it is not exercised end-to-end in CI.
func applyPOSIXReboot(ctx context.Context, backend posixShellBackend, params map[string]any) (ApplyResult, error) {
	timeout := 300
	if raw, ok := params["timeout"].(int); ok && raw > 0 {
		timeout = raw
	}
	if raw, ok := params["timeout"].(int64); ok && raw > 0 {
		timeout = int(raw)
	}
	if raw, ok := params["timeout"].(float64); ok && raw > 0 {
		timeout = int(raw)
	}

	probe, err := backend.Probe(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	if probe.Init != "systemd" {
		return ApplyResult{}, NewMissingPrerequisiteError("reboot", RuntimeKindPOSIXShell,
			"requires systemd; no init system detected on the target")
	}

	// Issue the reboot. systemctl reboot does not return until shutdown begins;
	// the connection typically drops mid-command, surfacing as a transport
	// error that is expected here.
	_, _, _, _ = backend.RunPOSIXCommand(ctx, "systemctl reboot", nil)

	if err := posixRebootReconnect(ctx, backend, timeout, time.Now, time.Sleep); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Message: "rebooted; target reconnected"}, nil
}

// posixRebootReconnect polls a lightweight command until the target answers,
// signalling it has rebooted and the transport has reconnected. now and sleep
// are injected so the loop is unit-testable against fakes without real
// sleeping. Each RunPOSIXCommand funnels through the transport's one-shot
// reconnect-and-retry, so a returned nil error means the target is back.
func posixRebootReconnect(ctx context.Context, backend posixShellBackend, timeoutSecs int, now func() time.Time, sleep func(time.Duration)) error {
	deadline := now().Add(time.Duration(timeoutSecs) * time.Second)
	for {
		_, _, _, err := backend.RunPOSIXCommand(ctx, ":", nil)
		if err == nil {
			return nil
		}
		if now().After(deadline) {
			return fmt.Errorf("reboot: target did not reconnect within %ds: %w", timeoutSecs, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sleep(5 * time.Second)
	}
}