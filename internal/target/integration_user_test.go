//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_User exercises the user module against a real Windows
// endpoint over every configured transport (WinRM and/or SSH-to-Windows).
// Each transport gets its own subtest and is independently skipped when its
// env vars are unset.
//
// Coverage template (per transport):
//   - present:     initial apply, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift:       group membership change converges back to desired state
//   - absent:      user removal, idempotent re-check
func TestIntegration_User(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// The run ID suffix prevents collisions when multiple `go test` processes
		// share the same VM.
		testName := fmt.Sprintf("pf-test-%s", testRunID()[:12])

		// ---- Cleanup: remove the test user via raw PowerShell ----
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Remove-LocalUser -Name "%s" -ErrorAction SilentlyContinue`,
				testName,
			))
			if err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		desiredParams := map[string]any{
			"name":     testName,
			"password": "PreflightTest123!",
			"groups":   []any{"Users"},
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		mustExecute(t, tgt, "user-dryrun", "user", desiredParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms user was NOT created — the dry run must not mutate.
		if got := readUserOracle(t, runner, testName); got != "absent" {
			t.Fatalf("dry-run: independent oracle: expected 'absent', got %q", got)
		}

		// ================================================================
		// Branch: present — initial apply creates the user with password
		// and group membership
		// ================================================================
		mustExecute(t, tgt, "user-present", "user", desiredParams, ExecutionOptions{}, false, StatusChanged)

		if got := readUserOracle(t, runner, testName); got != "present" {
			t.Fatalf("independent oracle: expected 'present', got %q", got)
		}
		if !readUserGroupOracle(t, runner, testName, "Users") {
			t.Fatal("independent oracle: expected user to be member of Users group")
		}

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "user-idemp-check", "user", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "user-idemp-apply", "user", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: drift — remove user from group behind the module's back,
		// then verify that Check detects and Apply converges back
		// ================================================================
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
			`net localgroup Users "%s" /delete 2>&1 | Out-Null`, testName,
		))
		if err != nil {
			t.Fatalf("drift setup: net localgroup /delete failed: %v", err)
		}
		// Oracle confirms user is no longer in the Users group.
		if readUserGroupOracle(t, runner, testName, "Users") {
			t.Fatal("drift setup: expected user to NOT be in Users group after /delete")
		}

		// Check detects the drift (NeedsChange = true -> StatusChanged)
		mustExecute(t, tgt, "user-drift-check", "user", desiredParams, ExecutionOptions{}, true, StatusChanged)

		// Apply converges back to the desired state
		mustExecute(t, tgt, "user-drift-apply", "user", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Oracle confirms group membership is restored
		if !readUserGroupOracle(t, runner, testName, "Users") {
			t.Fatal("drift: independent oracle: expected user to be member of Users group after convergence")
		}

		// Confirm idempotence after convergence
		mustExecute(t, tgt, "user-drift-idemp", "user", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: absent — ensure absent removes the user
		// ================================================================
		absentParams := map[string]any{
			"name":   testName,
			"ensure": "absent",
		}

		mustExecute(t, tgt, "user-absent", "user", absentParams, ExecutionOptions{}, false, StatusChanged)

		// Verify via oracle that user is gone.
		if got := readUserOracle(t, runner, testName); got != "absent" {
			t.Fatalf("independent oracle: expected 'absent', got %q", got)
		}

		// Idempotent absent — re-check returns OK (already absent).
		mustExecute(t, tgt, "user-absent-idemp", "user", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// readUserOracle is an independent PowerShell oracle that checks whether a
// local user exists via Get-LocalUser. It is written separately from the
// module's Check script to serve as a truthful assertion source.
//
// Returns "present" when the user exists, or "absent" when the user does not.
func readUserOracle(t *testing.T, runner PowerShellRunner, username string) string {
	t.Helper()
	ctx := context.Background()

	out, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) { Write-Output 'absent'; exit 0 }
Write-Output 'present'
`, username))
	if err != nil {
		t.Fatalf("user oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// readUserGroupOracle is an independent PowerShell oracle that checks whether
// a user is a member of a local group via Get-LocalGroupMember. It is written
// separately from the module's Check script to serve as a truthful assertion
// source.
//
// Returns true when the user is a member of the group, false otherwise.
func readUserGroupOracle(t *testing.T, runner PowerShellRunner, username, group string) bool {
	t.Helper()
	ctx := context.Background()

	out, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$group = "%s"
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) { Write-Output 'false'; exit 0 }
$members = Get-LocalGroupMember -Group $group -ErrorAction SilentlyContinue
$member = $members | Where-Object { $_.Name -match ("(^|\\)" + [regex]::Escape($name) + "$") }
if ($null -eq $member) { Write-Output 'false'; exit 0 }
Write-Output 'true'
`, username, group))
	if err != nil {
		t.Fatalf("user group oracle script failed: %v", err)
	}
	return strings.TrimSpace(out) == "true"
}
