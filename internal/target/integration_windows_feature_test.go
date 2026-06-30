package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWinRMIntegration_WindowsFeature exercises the windows_feature (DISM) module
// against a real Windows endpoint over WinRM. It uses TelnetClient as a
// lightweight feature — Enable/Disable-WindowsOptionalFeature on TelnetClient
// is fast, does not require reboot, and is available on virtually all Windows
// editions.
//
// Gated by PREFLIGHT_TEST_WINRM and the sacrificial sentinel.
//
// The test captures the original feature state before making changes and
// restores it in t.Cleanup. Correctness is asserted via an independent
// Get-WindowsOptionalFeature oracle rather than the module's own Check().
func TestWinRMIntegration_WindowsFeature(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// TelnetClient is a well-known lightweight Windows optional feature that
	// is available on most editions and does not require a reboot or source
	// media when enabling/disabling.
	featureName := "TelnetClient"

	// ---- Capture the original feature state ----
	origState := readWindowsFeatureOracle(t, tgt, featureName)
	t.Logf("original state of %s: %s", featureName, origState)

	// ---- Cleanup: restore original feature state ----
	t.Cleanup(func() {
		switch origState {
		case "Enabled":
			_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
				`Enable-WindowsOptionalFeature -Online -FeatureName "%s" -LimitAccess -NoRestart | Out-Null`,
				featureName,
			))
			if err != nil {
				t.Logf("cleanup enable %s: %v", featureName, err)
			}
		default: // "Disabled" or unknown — safe to disable
			_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
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

	// ---- Step 1: Apply (toggle feature state) ----
	params := map[string]any{
		"name":   featureName,
		"ensure": targetEnsure,
	}

	result, err := tgt.Execute(ctx, "windows-feature-apply", "windows_feature", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("windows_feature apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	gotState := readWindowsFeatureOracle(t, tgt, featureName)
	if targetEnsure == "present" && gotState != "Enabled" {
		t.Fatalf("independent oracle: expected feature to be Enabled, got %q", gotState)
	}
	if targetEnsure == "absent" && gotState != "Disabled" {
		t.Fatalf("independent oracle: expected feature to be Disabled, got %q", gotState)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "windows-feature-recheck", "windows_feature", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "windows-feature-reapply", "windows_feature", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Toggle back to the original state ----
	// If the feature was originally Enabled we need ensure=present to restore it.
	// If it was originally Disabled (or unknown) we need ensure=absent.
	originalEnsure := "absent"
	if origState == "Enabled" {
		originalEnsure = "present"
	}

	restoreParams := map[string]any{
		"name":   featureName,
		"ensure": originalEnsure,
	}

	result, err = tgt.Execute(ctx, "windows-feature-restore", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature restore: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("windows_feature restore: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Verify via oracle ----
	gotState = readWindowsFeatureOracle(t, tgt, featureName)
	if originalEnsure == "present" && gotState != "Enabled" {
		t.Fatalf("independent oracle after restore: expected feature to be Enabled, got %q", gotState)
	}
	if originalEnsure == "absent" && gotState != "Disabled" {
		t.Fatalf("independent oracle after restore: expected feature to be Disabled, got %q", gotState)
	}

	// ---- Step 7: Idempotency — re-check on restored state ----
	result, err = tgt.Execute(ctx, "windows-feature-restore-recheck", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature restore re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature restore re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 8: Idempotency — re-apply on restored state ----
	result, err = tgt.Execute(ctx, "windows-feature-restore-reapply", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature restore re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature restore re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}
}

// readWindowsFeatureOracle is an independent PowerShell oracle that reads a
// Windows optional feature's state via Get-WindowsOptionalFeature, without
// using the module's Check or Apply scripts. Returns "Enabled", "Disabled",
// or an empty string if the feature is not found.
func readWindowsFeatureOracle(t *testing.T, tgt *WinRMTarget, featureName string) string {
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
