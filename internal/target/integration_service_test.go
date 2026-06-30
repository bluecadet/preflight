//go:build integration

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

// TestIntegration_Service exercises the service module against a real
// Windows endpoint over every configured transport (WinRM and/or
// SSH-to-Windows). Each transport gets its own subtest and is independently
// skipped when its env vars are unset.
//
// The service module manages startup_type and state of an existing Windows
// service but does not create or delete services, so the test creates a
// sacrificial test service via sc.exe in its setup step.
//
// Coverage template (per transport):
//   - present:     apply startup_type=automatic, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift/update: change startup_type to manual, then state to disabled
//   - idempotent:  confirm final state is stable
func TestIntegration_Service(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// The run ID suffix prevents collisions when multiple `go test`
		// processes share the same VM.
		serviceName := fmt.Sprintf("pf-test-%s-svc", testRunID()[:12])

		// ---- Setup: create the test service if it does not already exist ----
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$svc = Get-Service -Name "%s" -ErrorAction SilentlyContinue
if ($null -eq $svc) {
  sc.exe create "%s" binPath= "C:\Windows\System32\svchost.exe" start= demand | Out-Null
}
`, serviceName, serviceName))
		if err != nil {
			t.Fatalf("setup create test service: %v", err)
		}

		// ---- Cleanup: remove the test service ----
		// NOTE: forEachTransport already registers t.Cleanup to close tgt,
		// so we only delete the service here.
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`sc.exe delete "%s" 2>&1 | Out-Null`, serviceName,
			))
			if err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		// ================================================================
		// Branch: present — initial apply sets startup_type to automatic
		// ================================================================
		desiredParams := map[string]any{
			"name":         serviceName,
			"startup_type": "automatic",
		}

		mustExecute(t, tgt, "svc-present", "service", desiredParams, ExecutionOptions{}, false, StatusChanged)

		state := readServiceOracle(t, runner, serviceName)
		if !state.Present {
			t.Fatal("independent oracle: service not found after apply")
		}
		if state.StartupType != "Auto" {
			t.Fatalf("independent oracle: expected StartupType=Auto, got %q", state.StartupType)
		}

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "svc-idemp-check", "service", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "svc-idemp-apply", "service", desiredParams, ExecutionOptions{}, false, StatusOK)

		state = readServiceOracle(t, runner, serviceName)
		if state.StartupType != "Auto" {
			t.Fatalf("independent oracle: expected StartupType=Auto after idempotent, got %q", state.StartupType)
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		dryRunParams := map[string]any{
			"name":         serviceName,
			"startup_type": "disabled", // different from current Auto
		}
		mustExecute(t, tgt, "svc-dryrun", "service", dryRunParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms startup_type is still Auto — the dry run must not
		// mutate the target.
		state = readServiceOracle(t, runner, serviceName)
		if state.StartupType != "Auto" {
			t.Fatalf("independent oracle: expected StartupType=Auto after dry-run (no mutation), got %q", state.StartupType)
		}

		// ================================================================
		// Branch: drift/update — change startup_type to manual, then
		// change state to disabled
		// ================================================================
		updateParams := map[string]any{
			"name":         serviceName,
			"startup_type": "manual",
		}
		mustExecute(t, tgt, "svc-update-startup-manual", "service", updateParams, ExecutionOptions{}, false, StatusChanged)
		state = readServiceOracle(t, runner, serviceName)
		if state.StartupType != "Manual" {
			t.Fatalf("independent oracle: expected StartupType=Manual after update, got %q", state.StartupType)
		}

		// Apply state=disabled (stops the service and sets startup to Disabled)
		disabledParams := map[string]any{
			"name":  serviceName,
			"state": "disabled",
		}
		mustExecute(t, tgt, "svc-update-state-disabled", "service", disabledParams, ExecutionOptions{}, false, StatusChanged)
		state = readServiceOracle(t, runner, serviceName)
		if state.State != "Stopped" {
			t.Fatalf("independent oracle: expected State=Stopped, got %q", state.State)
		}
		if state.StartupType != "Disabled" {
			t.Fatalf("independent oracle: expected StartupType=Disabled, got %q", state.StartupType)
		}

		// ================================================================
		// Branch: idempotent — re-check and re-apply on state=disabled
		// ================================================================
		mustExecute(t, tgt, "svc-idemp-disabled-check", "service", disabledParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "svc-idemp-disabled-apply", "service", disabledParams, ExecutionOptions{}, false, StatusOK)
	})
}

// readServiceOracle is an independent PowerShell oracle that reads a service's
// status and startup type directly via Get-Service and Get-CimInstance,
// without using the module's Check or Apply scripts. This provides a truthful
// assertion source independent of the module implementation.
func readServiceOracle(t *testing.T, tgt PowerShellRunner, serviceName string) serviceOracleResult {
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
