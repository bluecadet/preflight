package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWinRMIntegration_PowerPlan exercises the power_plan module against a
// real Windows endpoint over WinRM. Since power_plan mutates global machine
// state (the active power scheme), the test captures the original active
// scheme before making changes and restores it in t.Cleanup.
//
// The test uses independent powercfg /list and powercfg /getactivescheme
// calls as oracles rather than relying on the module's own Check().
func TestWinRMIntegration_PowerPlan(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The run ID suffix prevents collisions when multiple `go test` processes
	// share the same VM.
	testName := fmt.Sprintf("pf-test-%s-pp", testRunID()[:12])

	// ---- Capture the original active scheme so we can restore it later ----
	origGUID := getActivePowerSchemeOracle(t, tgt)
	t.Logf("original active scheme GUID: %s", origGUID)

	// ---- Cleanup: restore original active scheme and delete test scheme ----
	t.Cleanup(func() {
		// Restore the original active scheme first (important: do this
		// before trying to delete the test scheme, in case the test scheme
		// is currently active).
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`& powercfg.exe /setactive "%s" 2>&1 | Out-Null`, origGUID,
		))
		if err != nil {
			t.Logf("cleanup restore active scheme: %v", err)
		}
		// Now delete the test scheme if it still exists.
		_, err = tgt.RunPowerShell(ctx, fmt.Sprintf(`
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

	// ---- Step 1: Create a new power scheme and activate it ----
	params := map[string]any{
		"name":     testName,
		"ensure":   "present",
		"activate": true,
		"base":     "balanced",
	}

	result, err := tgt.Execute(ctx, "powerplan-apply", "power_plan", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("power_plan apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	testGUID := getNamedSchemeGUIDOracle(t, tgt, testName)
	if testGUID == "" {
		t.Fatal("independent oracle: test scheme not found via powercfg /list")
	}
	activeGUID := getActivePowerSchemeOracle(t, tgt)
	if activeGUID != testGUID {
		t.Fatalf("independent oracle: expected active scheme to be test scheme (%s), got %s", testGUID, activeGUID)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "powerplan-recheck", "power_plan", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("power_plan re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "powerplan-reapply", "power_plan", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("power_plan re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Ensure absent removes the scheme ----
	absentParams := map[string]any{
		"name":   testName,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "powerplan-absent", "power_plan", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("power_plan absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Verify via oracle that the scheme no longer exists
	if guid := getNamedSchemeGUIDOracle(t, tgt, testName); guid != "" {
		t.Fatal("independent oracle: expected test scheme to be absent, but it still exists")
	}

	// ---- Step 6: Idempotency — ensure absent again is a no-op ----
	result, err = tgt.Execute(ctx, "powerplan-absent-idempotent", "power_plan", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan absent (idempotent): %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("power_plan absent (idempotent): expected StatusOK, got %q: %s", result.Status, result.Message)
	}
}

// getActivePowerSchemeOracle runs powercfg /getactivescheme and returns
// the GUID of the currently active power scheme. This is an independent
// oracle separate from the module's own check logic.
func getActivePowerSchemeOracle(t *testing.T, tgt PowerShellRunner) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, `
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
func getNamedSchemeGUIDOracle(t *testing.T, tgt PowerShellRunner, name string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
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
