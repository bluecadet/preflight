package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// serviceOracleResult holds the fields read by the independent
// Get-Service / Get-CimInstance oracle.
type serviceOracleResult struct {
	Present     bool
	State       string
	StartupType string
}

// TestWinRMIntegration_Service exercises the service module against a real
// Windows endpoint over WinRM. It creates a test service under the pf-test-*
// naming convention, modifies its startup type and state, and verifies
// correctness via an independent Get-Service/CIM oracle.
//
// Gated by PREFLIGHT_TEST_WINRM and the sacrificial sentinel.
func TestWinRMIntegration_Service(t *testing.T) {
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
	serviceName := fmt.Sprintf("pf-test-%s-svc", testRunID()[:12])

	// ---- Setup: create the test service if it does not already exist ----
	_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$svc = Get-Service -Name "%s" -ErrorAction SilentlyContinue
if ($null -eq $svc) {
  sc.exe create "%s" binPath= "C:\Windows\System32\svchost.exe" start= demand | Out-Null
}
`, serviceName, serviceName))
	if err != nil {
		t.Fatalf("setup create test service: %v", err)
	}

	// ---- Cleanup: remove the test service ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`sc.exe delete "%s" 2>&1 | Out-Null`, serviceName,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 1: Apply — change startup_type to automatic ----
	params := map[string]any{
		"name":         serviceName,
		"startup_type": "automatic",
	}

	result, err := tgt.Execute(ctx, "svc-apply-startup-automatic", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service apply startup_type=automatic: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("service apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	state := readServiceOracle(t, tgt, serviceName)
	if !state.Present {
		t.Fatal("independent oracle: service not found after apply")
	}
	if state.StartupType != "Auto" {
		t.Fatalf("independent oracle: expected StartupType=Auto, got %q", state.StartupType)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "svc-recheck-startup-automatic", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("service re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "svc-reapply-startup-automatic", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("service re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Apply — change startup_type to manual ----
	params = map[string]any{
		"name":         serviceName,
		"startup_type": "manual",
	}

	result, err = tgt.Execute(ctx, "svc-apply-startup-manual", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service apply startup_type=manual: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("service apply startup_type=manual: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Verify via independent oracle ----
	state = readServiceOracle(t, tgt, serviceName)
	if state.StartupType != "Manual" {
		t.Fatalf("independent oracle: expected StartupType=Manual, got %q", state.StartupType)
	}

	// ---- Step 7: Apply — set state to disabled (stopped + disabled startup) ----
	params = map[string]any{
		"name":  serviceName,
		"state": "disabled",
	}

	result, err = tgt.Execute(ctx, "svc-apply-state-disabled", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service apply state=disabled: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("service apply state=disabled: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 8: Verify via independent oracle ----
	state = readServiceOracle(t, tgt, serviceName)
	if state.State != "Stopped" {
		t.Fatalf("independent oracle: expected State=Stopped, got %q", state.State)
	}
	if state.StartupType != "Disabled" {
		t.Fatalf("independent oracle: expected StartupType=Disabled, got %q", state.StartupType)
	}

	// ---- Step 9: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "svc-recheck-state-disabled", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service re-check state=disabled: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("service re-check state=disabled: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 10: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "svc-reapply-state-disabled", "service", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("service re-apply state=disabled: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("service re-apply state=disabled: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}
}

// readServiceOracle is an independent PowerShell oracle that reads a service's
// status and startup type directly via Get-Service and Get-CimInstance,
// without using the module's Check or Apply scripts. This provides a truthful
// assertion source independent of the module implementation.
func readServiceOracle(t *testing.T, tgt *WinRMTarget, serviceName string) serviceOracleResult {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$svc = Get-Service -Name $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $svc) {
  Write-Output "absent|||"
  exit 0
}
$cim = Get-CimInstance Win32_Service -Filter ("Name='" + $name.Replace("'", "''") + "'")
$startMode = if ($null -eq $cim) { '' } else { $cim.StartMode }
Write-Output ("present|" + $svc.Status + "|" + $startMode)
`, serviceName))
	if err != nil {
		t.Fatalf("service oracle script failed: %v", err)
	}

	parts := strings.SplitN(strings.TrimSpace(out), "|", 4)
	if len(parts) < 1 {
		t.Fatalf("service oracle: unexpected output format: %q", out)
	}

	result := serviceOracleResult{
		Present: parts[0] == "present",
	}
	if result.Present && len(parts) >= 3 {
		result.State = parts[1]
		result.StartupType = parts[2]
	}
	return result
}
