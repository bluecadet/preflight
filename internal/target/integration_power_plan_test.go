//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_PowerPlan exercises the power_plan module against a real
// Windows endpoint over every configured transport (WinRM and/or SSH-to-Windows).
// Each transport gets its own subtest and is independently skipped when its
// env vars are unset.
//
// Coverage template (per transport):
//   - present:     initial apply, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift:       active scheme change converges back to desired state
//   - absent:      scheme removal, idempotent re-check
func TestIntegration_PowerPlan(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// The run ID suffix prevents collisions when multiple `go test` processes
		// share the same VM.
		testName := fmt.Sprintf("pf-test-%s-pp", testRunID()[:12])

		// ---- Capture the original active scheme so we can restore it later ----
		origGUID := getActivePowerSchemeOracle(t, runner)
		t.Logf("original active scheme GUID: %s", origGUID)

		// ---- Cleanup: restore original active scheme and delete test scheme ----
		t.Cleanup(func() {
			// Restore the original active scheme first (important: do this
			// before trying to delete the test scheme, in case the test scheme
			// is currently active).
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`& powercfg.exe /setactive "%s" 2>&1 | Out-Null`, origGUID,
			))
			if err != nil {
				t.Logf("cleanup restore active scheme: %v", err)
			}
			// Now delete the test scheme if it still exists.
			_, err = runner.RunPowerShell(ctx, fmt.Sprintf(`
$targetName = '%s'
foreach ($line in (& powercfg.exe /list 2>&1)) {
  if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)') {
    if ($matches[2] -eq $targetName) {
      & powercfg.exe /delete $matches[1] 2>&1 | Out-Null
    }
  }
}
`, testName))
			if err != nil {
				t.Logf("cleanup delete test scheme: %v", err)
			}
		})

		desiredParams := map[string]any{
			"name":     testName,
			"ensure":   "present",
			"activate": true,
			"base":     "balanced",
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		mustExecute(t, tgt, "powerplan-dryrun", "power_plan", desiredParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms scheme was NOT created — the dry run must not mutate.
		if guid := getNamedSchemeGUIDOracle(t, runner, testName); guid != "" {
			t.Fatal("dry-run: independent oracle: test scheme should not exist before present branch")
		}

		// ================================================================
		// Branch: present — initial apply creates and activates the scheme
		// ================================================================
		mustExecute(t, tgt, "powerplan-present", "power_plan", desiredParams, ExecutionOptions{}, false, StatusChanged)

		testGUID := getNamedSchemeGUIDOracle(t, runner, testName)
		if testGUID == "" {
			t.Fatal("independent oracle: test scheme not found via powercfg /list")
		}
		activeGUID := getActivePowerSchemeOracle(t, runner)
		if activeGUID != testGUID {
			t.Fatalf("independent oracle: expected active scheme to be test scheme (%s), got %s", testGUID, activeGUID)
		}

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "powerplan-idemp-check", "power_plan", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "powerplan-idemp-apply", "power_plan", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: drift — change active scheme behind the module's back,
		// then verify convergence
		// ================================================================
		// Activate the original scheme to create drift.
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
			`& powercfg.exe /setactive "%s" 2>&1 | Out-Null`, origGUID,
		))
		if err != nil {
			t.Fatalf("drift setup: powercfg /setactive failed: %v", err)
		}
		// Oracle confirms the original scheme is now active.
		activeGUID = getActivePowerSchemeOracle(t, runner)
		if activeGUID != origGUID {
			t.Fatalf("drift setup: expected active scheme to be original (%s), got %s", origGUID, activeGUID)
		}

		// Check detects the drift and applies convergence.
		mustExecute(t, tgt, "powerplan-drift", "power_plan", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Oracle confirms test scheme is active again.
		testGUID = getNamedSchemeGUIDOracle(t, runner, testName)
		activeGUID = getActivePowerSchemeOracle(t, runner)
		if activeGUID != testGUID {
			t.Fatalf("drift: expected active scheme to be test scheme (%s), got %s", testGUID, activeGUID)
		}

		// Confirm idempotence after convergence.
		mustExecute(t, tgt, "powerplan-drift-idemp", "power_plan", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: absent — remove the scheme entirely
		// ================================================================
		absentParams := map[string]any{
			"name":   testName,
			"ensure": "absent",
		}

		mustExecute(t, tgt, "powerplan-absent", "power_plan", absentParams, ExecutionOptions{}, false, StatusChanged)

		// Verify via oracle that the scheme no longer exists.
		if guid := getNamedSchemeGUIDOracle(t, runner, testName); guid != "" {
			t.Fatal("independent oracle: expected test scheme to be absent, but it still exists")
		}

		// Idempotent absent — re-check returns OK (already absent).
		mustExecute(t, tgt, "powerplan-absent-idemp", "power_plan", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// getActivePowerSchemeOracle runs powercfg /getactivescheme and returns
// the GUID of the currently active power scheme. This is an independent
// oracle separate from the module's own check logic.
func getActivePowerSchemeOracle(t *testing.T, runner PowerShellRunner) string {
	t.Helper()
	ctx := context.Background()

	out, err := runner.RunPowerShell(ctx, `
$line = & powercfg.exe /getactivescheme 2>&1
if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})') {
  Write-Output $matches[1]
  exit 0
}
Write-Output ''
`)
	if err != nil {
		t.Fatalf("getActivePowerSchemeOracle failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// getNamedSchemeGUIDOracle returns the GUID of the first power scheme whose
// name matches the given name, or an empty string if no match is found. Uses
// powercfg /list as an independent oracle.
func getNamedSchemeGUIDOracle(t *testing.T, runner PowerShellRunner, name string) string {
	t.Helper()
	ctx := context.Background()

	out, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$targetName = '%s'
foreach ($line in (& powercfg.exe /list 2>&1)) {
  if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)') {
    if ($matches[2] -eq $targetName) {
      Write-Output $matches[1]
      exit 0
    }
  }
}
Write-Output ''
`, name))
	if err != nil {
		t.Fatalf("getNamedSchemeGUIDOracle failed: %v", err)
	}
	return strings.TrimSpace(out)
}
