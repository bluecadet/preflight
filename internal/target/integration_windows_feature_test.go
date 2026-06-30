//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_WindowsFeature exercises the windows_feature (DISM) module
// against a real Windows endpoint over every configured transport (WinRM
// and/or SSH-to-Windows). Each transport gets its own subtest and is
// independently skipped when its env vars are unset.
//
// Uses TelnetClient as a lightweight feature — Enable/Disable-WindowsOptionalFeature
// on TelnetClient is fast, does not require reboot, and is available on
// virtually all Windows editions.
//
// Coverage template (per transport):
//   - present/absent: initial apply, verify via oracle
//   - idempotent:     re-check and re-apply both return StatusOK
//   - dry-run:        check-only predicts Changed, oracle confirms no mutation
//   - drift:          manual toggle, check detects, apply converges back
//   - restore:        toggle back to original state, verify, idempotent
//
// MUST run exclusively — never co-scheduled with other live-VM tests
// (per ADR-0015).
func TestIntegration_WindowsFeature(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		featureName := "TelnetClient"

		// ---- Capture the original feature state ----
		origState := readWindowsFeatureOracle(t, runner, featureName)
		t.Logf("original state of %s: %s", featureName, origState)

		// ---- Cleanup: restore original feature state ----
		t.Cleanup(func() {
			switch origState {
			case "Enabled":
				_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
					`Enable-WindowsOptionalFeature -Online -FeatureName "%s" -LimitAccess -NoRestart | Out-Null`,
					featureName,
				))
				if err != nil {
					t.Logf("cleanup enable %s: %v", featureName, err)
				}
			default: // "Disabled" or unknown — safe to disable
				_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
					`Disable-WindowsOptionalFeature -Online -FeatureName "%s" -NoRestart | Out-Null`,
					featureName,
				))
				if err != nil {
					t.Logf("cleanup disable %s: %v", featureName, err)
				}
			}
		})

		// Determine which direction counts as a change. If the feature is already
		// enabled we disable it; if it's disabled we enable it.
		targetEnsure := "present"
		if origState == "Enabled" {
			targetEnsure = "absent"
		}

		desiredParams := map[string]any{
			"name":   featureName,
			"ensure": targetEnsure,
		}

		// ================================================================
		// Branch: present/absent — initial apply toggles the feature
		// ================================================================
		// The initial apply must handle the WinRM DISM servicing limitation
		// (symlink restriction under a network logon token).
		result, err := tgt.Execute(ctx, "windows-feature-apply", "windows_feature", desiredParams, ExecutionOptions{}, false, nil)
		if err != nil {
			if tgt.Transport() == TransportWinRM && isWinRMServicingUnsupported(err.Error()) {
				t.Skipf("DISM online servicing is unsupported over this WinRM session "+
					"(requires an interactive logon, e.g. CredSSP): %v", err)
			}
			t.Fatalf("windows_feature apply: %v", err)
		}
		if result.Status != StatusChanged {
			t.Fatalf("windows_feature apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
		}

		// Verify via independent oracle
		gotState := readWindowsFeatureOracle(t, runner, featureName)
		if targetEnsure == "present" && gotState != "Enabled" {
			t.Fatalf("independent oracle: expected feature to be Enabled, got %q", gotState)
		}
		if targetEnsure == "absent" && gotState != "Disabled" {
			t.Fatalf("independent oracle: expected feature to be Disabled, got %q", gotState)
		}

		// ================================================================
		// Branch: idempotent — re-check and re-apply both return StatusOK
		// ================================================================
		mustExecute(t, tgt, "windows-feature-idemp-check", "windows_feature", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "windows-feature-idemp-apply", "windows_feature", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		// The dry-run uses the restore params (the direction opposite to the
		// desired state). Since the feature is now in the desired toggled state,
		// check-only with the restore params should predict a change but not
		// actually mutate.
		restoreEnsure := "absent"
		if origState == "Enabled" {
			restoreEnsure = "present"
		}
		dryRunParams := map[string]any{
			"name":   featureName,
			"ensure": restoreEnsure,
		}
		mustExecute(t, tgt, "windows-feature-dryrun", "windows_feature", dryRunParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms the feature is still in the desired toggled state.
		gotState = readWindowsFeatureOracle(t, runner, featureName)
		if targetEnsure == "present" && gotState != "Enabled" {
			t.Fatalf("dry-run oracle: expected feature to still be Enabled, got %q", gotState)
		}
		if targetEnsure == "absent" && gotState != "Disabled" {
			t.Fatalf("dry-run oracle: expected feature to still be Disabled, got %q", gotState)
		}

		// ================================================================
		// Branch: drift — mutate behind the module's back, then verify
		// that Check detects the change and Apply converges back
		// ================================================================
		// Manually toggle the feature via PowerShell
		if targetEnsure == "present" {
			// Feature is Enabled, manually disable it
			_, err = runner.RunPowerShell(ctx, fmt.Sprintf(
				`Disable-WindowsOptionalFeature -Online -FeatureName "%s" -NoRestart | Out-Null`,
				featureName,
			))
		} else {
			// Feature is Disabled, manually enable it
			_, err = runner.RunPowerShell(ctx, fmt.Sprintf(
				`Enable-WindowsOptionalFeature -Online -FeatureName "%s" -LimitAccess -NoRestart | Out-Null`,
				featureName,
			))
		}
		if err != nil {
			if tgt.Transport() == TransportWinRM && isWinRMServicingUnsupported(err.Error()) {
				t.Skipf("DISM drift setup unsupported over this WinRM session: %v", err)
			}
			t.Fatalf("drift setup: PowerShell toggle failed: %v", err)
		}

		// Verify drift via oracle
		driftedState := readWindowsFeatureOracle(t, runner, featureName)
		if targetEnsure == "absent" && driftedState != "Enabled" {
			t.Fatalf("drift oracle: expected feature to be Enabled (drifted), got %q", driftedState)
		}
		if targetEnsure == "present" && driftedState != "Disabled" {
			t.Fatalf("drift oracle: expected feature to be Disabled (drifted), got %q", driftedState)
		}

		// Check detects the drift (NeedsChange = true → StatusChanged)
		mustExecute(t, tgt, "windows-feature-drift-check", "windows_feature", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Apply converges back to the desired state
		result, err = tgt.Execute(ctx, "windows-feature-drift-apply", "windows_feature", desiredParams, ExecutionOptions{}, false, nil)
		if err != nil {
			if tgt.Transport() == TransportWinRM && isWinRMServicingUnsupported(err.Error()) {
				t.Skipf("DISM drift apply unsupported over this WinRM session: %v", err)
			}
			t.Fatalf("windows_feature drift apply: %v", err)
		}
		if result.Status != StatusChanged {
			t.Fatalf("windows_feature drift apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
		}

		// Oracle confirms convergence
		gotState = readWindowsFeatureOracle(t, runner, featureName)
		if targetEnsure == "present" && gotState != "Enabled" {
			t.Fatalf("drift convergence oracle: expected feature to be Enabled, got %q", gotState)
		}
		if targetEnsure == "absent" && gotState != "Disabled" {
			t.Fatalf("drift convergence oracle: expected feature to be Disabled, got %q", gotState)
		}

		// Confirm idempotence after convergence
		mustExecute(t, tgt, "windows-feature-drift-idemp", "windows_feature", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: restore — toggle the feature back to its original state
		// ================================================================
		restoreParams := map[string]any{
			"name":   featureName,
			"ensure": restoreEnsure,
		}
		result, err = tgt.Execute(ctx, "windows-feature-restore", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
		if err != nil {
			t.Fatalf("windows_feature restore: %v", err)
		}
		if result.Status != StatusChanged {
			t.Fatalf("windows_feature restore: expected StatusChanged, got %q: %s", result.Status, result.Message)
		}

		// Verify via oracle
		gotState = readWindowsFeatureOracle(t, runner, featureName)
		if restoreEnsure == "present" && gotState != "Enabled" {
			t.Fatalf("restore oracle: expected feature to be Enabled, got %q", gotState)
		}
		if restoreEnsure == "absent" && gotState != "Disabled" {
			t.Fatalf("restore oracle: expected feature to be Disabled, got %q", gotState)
		}

		// Idempotent re-check after restore
		mustExecute(t, tgt, "windows-feature-restore-idemp-check", "windows_feature", restoreParams, ExecutionOptions{}, false, StatusOK)
		// Idempotent re-apply after restore
		mustExecute(t, tgt, "windows-feature-restore-idemp-apply", "windows_feature", restoreParams, ExecutionOptions{}, false, StatusOK)
	})
}

// readWindowsFeatureOracle is an independent PowerShell oracle that reads a
// Windows optional feature's state via Get-WindowsOptionalFeature, without
// using the module's Check or Apply scripts. Returns "Enabled", "Disabled",
// or an empty string if the feature is not found.
func readWindowsFeatureOracle(t *testing.T, tgt PowerShellRunner, featureName string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$feature = Get-WindowsOptionalFeature -Online -FeatureName $name -ErrorAction SilentlyContinue
if ($null -eq $feature) {
  Write-Output "missing"
  exit 0
}
Write-Output $feature.State.ToString()
`, featureName))
	if err != nil {
		t.Fatalf("windows feature oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}