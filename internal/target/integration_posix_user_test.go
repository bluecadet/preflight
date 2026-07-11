//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_POSIXUser exercises the user module over SSH against a real
// POSIX target. The user module requires root (requires_root), so every
// execute uses become to root. The harness connects as the NOPASSWD-sudo user
// provisioned by the CI containers (pf-admin), so bare become means root.
//
// Coverage (acceptance criteria — create, group add, absent, password-on-create):
//   - create:          ensure present with password + group → StatusChanged;
//                      oracle confirms user exists, password is set, and group
//                      membership is held.
//   - idempotent:      re-check and re-apply return StatusOK.
//   - group add:       apply with a new group on the existing user →
//                      StatusChanged; oracle confirms membership.
//   - password drift:  apply with a *different* password (plus the group add,
//                      so Apply actually runs) leaves the original password
//                      hash untouched — existing-user password drift is a
//                      documented limitation, not corrected.
//   - absent:          ensure absent removes the user → StatusChanged;
//                      idempotent re-check returns OK.
func TestIntegration_POSIXUser(t *testing.T) {
	forEachPOSIXTarget(t, func(t *testing.T, tgt *SSHTarget) {
		ctx := context.Background()
		name := fmt.Sprintf("pfuser-%s", testRunID()[:12])
		group := fmt.Sprintf("pfgrp-%s", testRunID()[:12])
		// Bare become means root on POSIX (see §5).
		become := ExecutionOptions{Become: &BecomeOptions{Enabled: true}}

		// Create a dedicated test group so the test is self-contained and does
		// not depend on any system group existing on the target distro.
		posixRun(t, tgt, fmt.Sprintf("sudo -n groupadd %q 2>/dev/null || true", group))

		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo -n userdel %q 2>/dev/null || true", name), nil)
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo -n groupdel %q 2>/dev/null || true", group), nil)
		})

		// ================================================================
		// create: ensure present with password + group membership
		// ================================================================
		createParams := map[string]any{
			"name":     name,
			"password": "PreflightTest123!",
			"groups":   []any{group},
		}
		mustExecute(t, tgt, "user-create", "user", createParams, become, false, StatusChanged)

		if !posixUserExistsOracle(t, tgt, name) {
			t.Fatalf("user %q was not created", name)
		}
		if !posixUserPasswordSetOracle(t, tgt, name) {
			t.Fatalf("user %q: password was not set on creation", name)
		}
		if !posixUserInGroupOracle(t, tgt, name, group) {
			t.Fatalf("user %q: expected membership in group %q after create", name, group)
		}
		hashAfterCreate := posixUserShadowHashOracle(t, tgt, name)

		// ================================================================
		// idempotent: re-check and re-apply return OK
		// ================================================================
		mustExecute(t, tgt, "user-idemp-check", "user", createParams, become, false, StatusOK)
		mustExecute(t, tgt, "user-idemp-apply", "user", createParams, become, false, StatusOK)

		// ================================================================
		// group add: apply with a second group on the existing user
		// ================================================================
		group2 := fmt.Sprintf("pfgrp2-%s", testRunID()[:12])
		posixRun(t, tgt, fmt.Sprintf("sudo -n groupadd %q 2>/dev/null || true", group2))
		t.Cleanup(func() {
			_, _, _, _ = tgt.run(ctx, fmt.Sprintf("sudo -n groupdel %q 2>/dev/null || true", group2), nil)
		})

		groupAddParams := map[string]any{
			"name":     name,
			"password": "PreflightTest123!",
			"groups":   []any{group, group2},
		}
		mustExecute(t, tgt, "user-group-add", "user", groupAddParams, become, false, StatusChanged)
		if !posixUserInGroupOracle(t, tgt, name, group2) {
			t.Fatalf("user %q: expected membership in group %q after group add", name, group2)
		}
		// Group add is additive: the first group must still be held.
		if !posixUserInGroupOracle(t, tgt, name, group) {
			t.Fatalf("user %q: group add dropped existing membership in %q", name, group)
		}

		// ================================================================
		// password drift: re-apply with a DIFFERENT password. Apply runs
		// (no change is needed above — this is idempotent OK territory), so
		// instead exercise the limitation by changing the password behind the
		// module's back and confirming the module's idempotent re-apply does
		// not reset it.
		// ================================================================
		posixRunWithStdinOracle(t, tgt, "sudo -n chpasswd", fmt.Sprintf("%s:DriftedPass456\n", name))
		hashAfterDrift := posixUserShadowHashOracle(t, tgt, name)
		if hashAfterDrift == hashAfterCreate {
			t.Fatal("drift setup: expected password hash to change after chpasswd, but it did not")
		}
		// Idempotent re-apply with the original create params: Check returns
		// OK (user exists, all groups held), so Apply never runs and the
		// drifted password is left in place.
		mustExecute(t, tgt, "user-drift-idemp", "user", createParams, become, false, StatusOK)
		hashAfterReapply := posixUserShadowHashOracle(t, tgt, name)
		if hashAfterReapply != hashAfterDrift {
			t.Fatalf("password drift limitation violated: hash changed after idempotent re-apply (drift=%q, after=%q)", hashAfterDrift, hashAfterReapply)
		}

		// ================================================================
		// absent: ensure absent removes the user
		// ================================================================
		absentParams := map[string]any{"name": name, "ensure": "absent"}
		mustExecute(t, tgt, "user-absent", "user", absentParams, become, false, StatusChanged)
		if posixUserExistsOracle(t, tgt, name) {
			t.Fatalf("user %q still exists after absent", name)
		}
		mustExecute(t, tgt, "user-absent-idemp", "user", absentParams, become, false, StatusOK)
	})
}

// posixUserExistsOracle is an independent oracle: returns true when the named
// user exists, via id(1).
func posixUserExistsOracle(t *testing.T, tgt *SSHTarget, name string) bool {
	t.Helper()
	code := posixExitCode(t, tgt, fmt.Sprintf("id %q >/dev/null 2>&1", name))
	return code == 0
}

// posixUserInGroupOracle is an independent oracle: returns true when the named
// user is a member of the named group, via id -nG.
func posixUserInGroupOracle(t *testing.T, tgt *SSHTarget, name, group string) bool {
	t.Helper()
	out := posixRun(t, tgt, fmt.Sprintf("id -nG %q", name))
	for _, g := range strings.Fields(out) {
		if g == group {
			return true
		}
	}
	return false
}

// posixUserShadowHashOracle is an independent oracle that reads the password
// hash field from /etc/shadow (via getent, as root through the NOPASSWD sudo
// user). Used both to confirm a password was set on creation and to detect
// whether the module reset it on a subsequent apply.
func posixUserShadowHashOracle(t *testing.T, tgt *SSHTarget, name string) string {
	t.Helper()
	out := posixRun(t, tgt, fmt.Sprintf("sudo -n getent shadow %q 2>/dev/null | cut -d: -f2", name))
	return strings.TrimSpace(out)
}

// posixUserPasswordSetOracle reports whether the user has a usable (non-empty,
// non-locked) password hash, per the shadow field conventions: "!" / "!!" means
// locked, "*" means no password, anything else (e.g. "$6$...") is a set hash.
func posixUserPasswordSetOracle(t *testing.T, tgt *SSHTarget, name string) bool {
	t.Helper()
	field := posixUserShadowHashOracle(t, tgt, name)
	if field == "" || field == "*" || strings.HasPrefix(field, "!") {
		return false
	}
	return true
}

// posixRunWithStdinOracle runs a shell command on the target with the given
// stdin and fails the test on any error or non-zero exit. Used for the
// password-drift setup (chpasswd reads "name:pass" from stdin).
func posixRunWithStdinOracle(t *testing.T, tgt *SSHTarget, cmd, stdin string) {
	t.Helper()
	ctx := context.Background()
	_, stderr, code, err := tgt.run(ctx, cmd, []byte(stdin))
	if err != nil {
		t.Fatalf("oracle command failed: %v\nstderr: %s", err, stderr)
	}
	if code != 0 {
		t.Fatalf("oracle command exited %d: %s", code, strings.TrimSpace(stderr))
	}
}
