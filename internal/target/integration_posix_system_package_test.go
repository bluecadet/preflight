//go:build integration

package target

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_POSIXSystemPackage exercises the system_package module over
// SSH against a real POSIX target, covering both apt (Ubuntu container) and
// dnf (Rocky container) through the single forEachPOSIXTarget harness. The
// module autodetects the manager from the cached probe, so the same test
// logic runs on both. system_package is requires_root and root SSH is
// disabled in the containers, so the mutating steps run as pf-admin (NOPASSWD
// sudo) with become enabled. Coverage:
//
//   - install:     ensure present installs a small package, oracle confirms
//   - idempotent:  re-check and re-apply both return StatusOK
//   - version pin: pin to the installed version is a no-op change
//   - dry-run:     a wrong-version pin predicts Changed, no mutation
//   - absent:      ensure absent removes the package, idempotent re-check
//
// Package choice: `tree` is in the base repos of both Ubuntu and Rocky, is
// small, and starts no services. The version-pin sub-case uses the installed
// version reported by the manager so the pin is satisfiable without network
// access to older versions.
func TestIntegration_POSIXSystemPackage(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		// Detect the manager via the oracle so the test picks the right
		// package name and version query. The module does the same from the
		// cached probe; this is the independent verification path.
		pm := strings.TrimSpace(posixRun(t, tgt, "if command -v apt-get >/dev/null 2>&1; then printf apt; elif command -v dnf >/dev/null 2>&1; then printf dnf; fi"))
		if pm == "" {
			t.Skip("target has neither apt nor dnf; system_package is not applicable")
		}

		// Pick a small, dependency-light package available in both distros.
		// `tree` is in the base repos of Ubuntu and Rocky and starts nothing.
		pkgName := "tree"

		// ensure a clean slate and clean up after. The cleanup uses the
		// become-enabled tgt (pf-admin can sudo), so removal succeeds.
		t.Cleanup(func() {
			removePackage(t, tgt, pm, pkgName)
		})
		removePackage(t, tgt, pm, pkgName)

		// become: pf-admin has NOPASSWD sudo to root. Root SSH is disabled in
		// the containers, so this is the only path to root for a requires_root
		// module.
		becomeOpts := ExecutionOptions{Become: &BecomeOptions{Enabled: true}}

		// ---- install: ensure present ----
		params := map[string]any{
			"packages": []any{
				map[string]any{"name": pkgName, "ensure": "present"},
			},
		}
		mustExecute(t, tgt, "pkg-install", "system_package", params, becomeOpts, false, StatusChanged)
		if !packageInstalled(t, tgt, pm, pkgName) {
			t.Fatalf("%s was not installed after ensure present", pkgName)
		}

		// ---- idempotent: re-check and re-apply both return OK ----
		mustExecute(t, tgt, "pkg-idemp-check", "system_package", params, becomeOpts, false, StatusOK)
		mustExecute(t, tgt, "pkg-idemp-apply", "system_package", params, becomeOpts, false, StatusOK)

		// ---- version pin: pin to the installed version (no-op change) ----
		installedVersion := installedVersion(t, tgt, pm, pkgName)
		if installedVersion == "" {
			t.Fatalf("could not determine installed version of %s to use as a pin", pkgName)
		}
		pinParams := map[string]any{
			"packages": []any{
				map[string]any{"name": pkgName, "version": installedVersion, "ensure": "present"},
			},
		}
		// Pinning to the already-installed version needs no change.
		mustExecute(t, tgt, "pkg-pin-noop-check", "system_package", pinParams, becomeOpts, false, StatusOK)
		mustExecute(t, tgt, "pkg-pin-noop-apply", "system_package", pinParams, becomeOpts, false, StatusOK)

		// ---- dry-run: a wrong-version pin predicts Changed, no mutation ----
		dryPinParams := map[string]any{
			"packages": []any{
				map[string]any{"name": pkgName, "version": "0.0.0-nonexistent", "ensure": "present"},
			},
		}
		mustExecute(t, tgt, "pkg-dryrun", "system_package", dryPinParams, becomeOpts, true, StatusChanged)
		if !packageInstalled(t, tgt, pm, pkgName) {
			t.Fatalf("dry-run removed %s", pkgName)
		}

		// ---- absent: remove the package ----
		absentParams := map[string]any{
			"packages": []any{
				map[string]any{"name": pkgName, "ensure": "absent"},
			},
		}
		mustExecute(t, tgt, "pkg-absent", "system_package", absentParams, becomeOpts, false, StatusChanged)
		if packageInstalled(t, tgt, pm, pkgName) {
			t.Fatalf("%s still installed after ensure absent", pkgName)
		}

		// ---- idempotent absent ----
		mustExecute(t, tgt, "pkg-absent-idemp", "system_package", absentParams, becomeOpts, false, StatusOK)
	})
}

// TestIntegration_POSIXSystemPackage_RequiresRoot asserts the requires_root
// enforcement fires when system_package runs as a non-root user without
// become. Connects as pf-nosudo (no sudo, non-root) and expects the task to
// fail with the requires-root-violation reason code before Check() runs.
func TestIntegration_POSIXSystemPackage_RequiresRoot(t *testing.T) {
	cfg, ok := getSSHPOSIXConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_POSIX_HOST / _USER / _PASS not set")
	}
	userCfg := *cfg
	userCfg.Username = "pf-nosudo"
	userCfg.Password = "preflight"
	tgt := NewSSHTarget(userCfg, nil)
	t.Cleanup(func() { _ = tgt.Close() })
	assertPOSIXSacrificialSentinel(t, tgt)

	ctx := context.Background()
	_, err := tgt.Execute(ctx, "pkg-root-violation", "system_package", map[string]any{
		"packages": []any{map[string]any{"name": "tree", "ensure": "present"}},
	}, ExecutionOptions{}, false, nil)
	if err == nil {
		t.Fatal("expected requires-root-violation error, got nil")
	}
	var be *BecomeEnvError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BecomeEnvError, got %T: %v", err, err)
	}
	if be.Class != ClassRequiresRootViolation {
		t.Errorf("class = %q, want %q", be.Class, ClassRequiresRootViolation)
	}
}

// removePackage removes pkg via the oracle path so the test starts clean.
// The containers disable root SSH and the harness connects as pf-admin
// (NOPASSWD sudo), so removal runs through sudo to get root.
func removePackage(t *testing.T, tgt *SSHTarget, pm, pkg string) {
	t.Helper()
	ctx := context.Background()
	switch pm {
	case "apt":
		_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo -n apt-get remove -y %q >/dev/null 2>&1 || true", pkg), nil)
	case "dnf":
		_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo -n dnf remove -y %q >/dev/null 2>&1 || true", pkg), nil)
	}
}

// packageInstalled reports whether pkg is installed via the oracle path.
func packageInstalled(t *testing.T, tgt *SSHTarget, pm, pkg string) bool {
	t.Helper()
	ctx := context.Background()
	var probe string
	switch pm {
	case "apt":
		probe = fmt.Sprintf("dpkg -s %q >/dev/null 2>&1 && echo yes || echo no", pkg)
	case "dnf":
		probe = fmt.Sprintf("rpm -q %q >/dev/null 2>&1 && echo yes || echo no", pkg)
	}
	stdout, _, _, err := tgt.run(ctx, probe, nil)
	if err != nil {
		t.Fatalf("packageInstalled oracle failed: %v", err)
	}
	return strings.TrimSpace(stdout) == "yes"
}

// installedVersion returns the native version string the module compares
// against: dpkg-query ${Version} for apt, rpm %{VERSION}-%{RELEASE} for dnf.
func installedVersion(t *testing.T, tgt *SSHTarget, pm, pkg string) string {
	t.Helper()
	ctx := context.Background()
	var probe string
	switch pm {
	case "apt":
		probe = fmt.Sprintf("dpkg-query -W -f='${Version}' %q 2>/dev/null || true", pkg)
	case "dnf":
		probe = fmt.Sprintf("rpm -q --qf '%%{VERSION}-%%{RELEASE}' %q 2>/dev/null || true", pkg)
	}
	stdout, _, _, err := tgt.run(ctx, probe, nil)
	if err != nil {
		t.Fatalf("installedVersion oracle failed: %v", err)
	}
	return strings.TrimSpace(stdout)
}
