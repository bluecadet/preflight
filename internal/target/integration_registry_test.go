//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_Registry exercises the registry module against a real
// Windows endpoint over every configured transport (WinRM and/or SSH-to-Windows).
// Each transport gets its own subtest and is independently skipped when its
// env vars are unset.
//
// Coverage template (per transport):
//   - present:     initial apply, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift:       value-data change converges back to desired state
//   - absent:      value removal, key removal, idempotent re-check
func TestIntegration_Registry(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		runKey := "IntegrationTest-" + testRunID()[:12]
		ns := `HKLM\SOFTWARE\PreflightTest\` + runKey
		nsProvider := `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest\` + runKey

		// ---- Cleanup: remove the per-run key ----
		t.Cleanup(func() {
			tgt.Close()
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Remove-Item -LiteralPath "%s" -Recurse -Force -ErrorAction SilentlyContinue`,
				nsProvider,
			))
			if err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		desiredParams := map[string]any{
			"path": ns,
			"values": []any{
				map[string]any{
					"name": "TestDword",
					"type": "dword",
					"data": 42,
				},
			},
		}

		// ================================================================
		// Branch: present — initial apply creates the key and value
		// ================================================================
		mustExecute(t, tgt, "registry-present", "registry", desiredParams, ExecutionOptions{}, false, StatusChanged)
		mustMatchOracle(t, runner, nsProvider, "TestDword", "42")

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "registry-idemp-check", "registry", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "registry-idemp-apply", "registry", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustMatchOracle(t, runner, nsProvider, "TestDword", "42")

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		dryRunParams := map[string]any{
			"path": ns,
			"values": []any{
				map[string]any{
					"name": "TestDword",
					"type": "dword",
					"data": 99, // different from current 42
				},
			},
		}
		mustExecute(t, tgt, "registry-dryrun", "registry", dryRunParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms value is still 42 — the dry run must not mutate.
		mustMatchOracle(t, runner, nsProvider, "TestDword", "42")

		// ================================================================
		// Branch: drift — mutate behind the module's back, then verify
		// that Check detects the change and Apply converges back
		// ================================================================
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
			`Set-ItemProperty -LiteralPath "%s" -Name "TestDword" -Value 99 -Type DWord`,
			nsProvider,
		))
		if err != nil {
			t.Fatalf("drift setup: Set-ItemProperty failed: %v", err)
		}
		mustMatchOracle(t, runner, nsProvider, "TestDword", "99")

		// Check detects the drift (NeedsChange = true → StatusChanged)
		mustExecute(t, tgt, "registry-drift-check", "registry", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Apply converges back to the desired state
		mustExecute(t, tgt, "registry-drift-apply", "registry", desiredParams, ExecutionOptions{}, false, StatusChanged)
		mustMatchOracle(t, runner, nsProvider, "TestDword", "42")

		// Confirm idempotence after convergence
		mustExecute(t, tgt, "registry-drift-idemp", "registry", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: absent — remove a value, then remove the entire key
		// ================================================================
		absentValueParams := map[string]any{
			"path": ns,
			"values": []any{
				map[string]any{
					"name":   "TestDword",
					"ensure": "absent",
				},
			},
		}
		mustExecute(t, tgt, "registry-absent-value", "registry", absentValueParams, ExecutionOptions{}, false, StatusChanged)
		mustMatchOracle(t, runner, nsProvider, "TestDword", "")

		absentKeyParams := map[string]any{
			"path":   ns,
			"ensure": "absent",
		}
		mustExecute(t, tgt, "registry-absent-key", "registry", absentKeyParams, ExecutionOptions{}, false, StatusChanged)
		mustMatchOracle(t, runner, nsProvider, "TestDword", "missing")

		// Idempotent absent — re-check returns OK (already absent)
		mustExecute(t, tgt, "registry-absent-idemp", "registry", absentKeyParams, ExecutionOptions{}, false, StatusOK)
	})
}

// readRegistryOracle is an independent PowerShell oracle that reads a registry
// value's data via Get-ItemProperty. It is written separately from the module's
// Check script to serve as a truthful assertion source.
//
// Returns the value data as a string, or "missing" when the path does not
// exist, or an empty string when the path exists but the named value does not.
//
// The provider path is injected into a PowerShell double-quoted string.
// PowerShell treats backslash as a literal character inside double quotes,
// so no additional escaping is needed.
func readRegistryOracle(t *testing.T, tgt PowerShellRunner, providerPath, valueName string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$path = "%s"
$name = "%s"
if (-not (Test-Path -LiteralPath $path)) {
  Write-Output 'missing'
  exit 0
}
$props = Get-ItemProperty -LiteralPath $path -ErrorAction SilentlyContinue
if ($null -eq $props) {
  Write-Output 'missing'
  exit 0
}
$prop = $props.PSObject.Properties[$name]
if ($null -eq $prop) {
  Write-Output ''
  exit 0
}
$value = $prop.Value
if ($null -eq $value) {
  Write-Output ''
  exit 0
}
if ($value -is [byte[]]) {
  Write-Output ($value | ForEach-Object { $_ })
  exit 0
}
Write-Output ([string]$value)
`, providerPath, valueName))
	if err != nil {
		t.Fatalf("oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}
