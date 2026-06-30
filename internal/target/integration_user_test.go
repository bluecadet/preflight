package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWinRMIntegration_User exercises the user module against a real Windows
// endpoint over WinRM. It is gated by PREFLIGHT_TEST_WINRM and the sacrificial
// sentinel, so it is inert on CI and on any dev machine without a configured VM.
//
// The test uses an independent PowerShell oracle (Get-LocalUser) to assert
// correctness rather than relying on the module's own Check().
func TestWinRMIntegration_User(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()
	// The sacrificial namespace for user names follows pf-test-* so the test
	// never touches non-test users. The shared run ID ensures uniqueness
	// across parallel runs without each test maintaining its own timestamp.
	testName := fmt.Sprintf("pf-test-%s", testRunID()[:12])

	// ---- Cleanup: remove the test user via raw PowerShell ----
	// Using a direct Remove-LocalUser command rather than the module under
	// test ensures cleanup is independent of module correctness. This follows
	// the pattern established by the registry test's cleanup.
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`Remove-LocalUser -Name "%s" -ErrorAction SilentlyContinue`,
			testName,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 1: Create a user with password and group membership ----
	params := map[string]any{
		"name":     testName,
		"password": "PreflightTest123!",
		"groups":   []any{"Users"},
	}

	result, err := tgt.Execute(ctx, "user-apply", "user", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("user apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	got := readUserOracle(t, tgt, testName)
	if got != "present" {
		t.Fatalf("independent oracle: expected 'present', got %q", got)
	}

	// ---- Step 3: Verify group membership via oracle ----
	if !readUserGroupOracle(t, tgt, testName, "Users") {
		t.Fatal("independent oracle: expected user to be member of Users group")
	}

	// ---- Step 4: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "user-recheck", "user", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("user re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "user-reapply", "user", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("user re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Ensure absent removes the user ----
	absentParams := map[string]any{
		"name":   testName,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "user-absent", "user", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("user absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 7: Verify via oracle that user is absent ----
	got = readUserOracle(t, tgt, testName)
	if got != "absent" {
		t.Fatalf("independent oracle: expected 'absent', got %q", got)
	}
}

// readUserOracle is an independent PowerShell oracle that checks whether a
// local user exists via Get-LocalUser. It is written separately from the
// module's Check script to serve as a truthful assertion source.
//
// Returns "present" when the user exists, or "absent" when the user does not.
func readUserOracle(t *testing.T, tgt *WinRMTarget, username string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
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
func readUserGroupOracle(t *testing.T, tgt *WinRMTarget, username, group string) bool {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
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
