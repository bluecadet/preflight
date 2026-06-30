//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_WingetPackage exercises the winget_package module against a
// real Windows endpoint over every configured transport (WinRM and/or
// SSH-to-Windows). Each transport gets its own subtest and is independently
// skipped when its env vars are unset.
//
// Coverage template (per transport):
//   - present:        initial apply, verify via oracle
//   - idempotent:     re-check and re-apply both return StatusOK
//   - dry-run:        check-only predicts Changed, oracle confirms no mutation
//   - drift:          package removed behind module's back, converges back
//   - absent:         remove package, verify via oracle, idempotent re-check
//
// The test uses 7zip.7zip from the winget source as the fixture because it is
// small (~1.5 MB), benign, stable, and available on all Windows editions that
// ship with winget. If winget is not available on the target the test skips
// gracefully.
func TestIntegration_WingetPackage(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		pkgID := "7zip.7zip"
		pkgSource := "winget"

		// ---- Cleanup: ensure the test package is uninstalled ----
		t.Cleanup(func() {
			// Note: forEachTransport already handles tgt.Close() — do not call it here.
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
				pkgID,
			))
			if err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		// ---- Guard: winget availability ----
		if !isWingetAvailable(ctx, runner) {
			t.Skip("winget.exe not found on target; skipping winget_package integration test")
		}

		// ---- Ensure package is not already installed ----
		// (in case a previous test run was interrupted)
		_, _ = runner.RunPowerShell(ctx, fmt.Sprintf(
			`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
			pkgID,
		))

		// ---- Verify package absent before test via oracle ----
		if isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to be absent before test")
		}

		desiredParams := map[string]any{
			"packages": []any{
				map[string]any{
					"id":     pkgID,
					"source": pkgSource,
				},
			},
		}

		// ================================================================
		// Branch: present — initial apply installs the package
		// ================================================================
		mustExecute(t, tgt, "winget-present", "winget_package", desiredParams, ExecutionOptions{}, false, StatusChanged)
		if !isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to be installed after apply")
		}

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "winget-idemp-check", "winget_package", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "winget-idemp-apply", "winget_package", desiredParams, ExecutionOptions{}, false, StatusOK)
		if !isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to remain installed after idempotent re-apply")
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		dryRunAbsentParams := map[string]any{
			"packages": []any{
				map[string]any{
					"id":     pkgID,
					"ensure": "absent",
				},
			},
		}
		mustExecute(t, tgt, "winget-dryrun", "winget_package", dryRunAbsentParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms package is still installed — the dry run must not mutate.
		if !isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to remain installed after dry-run (no mutation)")
		}

		// ================================================================
		// Branch: drift — uninstall the package behind the module's back,
		// then verify that Check detects the change and Apply converges
		// back
		// ================================================================
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
			`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
			pkgID,
		))
		if err != nil {
			t.Fatalf("drift setup: uninstall failed: %v", err)
		}

		// Oracle confirms package was removed by the drift side-effect
		if isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to be absent after drift uninstall")
		}

		// Check detects the drift (package is absent, desired is present → Changed)
		mustExecute(t, tgt, "winget-drift-check", "winget_package", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Apply converges back to the desired state
		mustExecute(t, tgt, "winget-drift-apply", "winget_package", desiredParams, ExecutionOptions{}, false, StatusChanged)
		if !isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to be re-installed after drift convergence")
		}

		// Confirm idempotence after convergence
		mustExecute(t, tgt, "winget-drift-idemp", "winget_package", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: absent — remove the package
		// ================================================================
		absentParams := map[string]any{
			"packages": []any{
				map[string]any{
					"id":     pkgID,
					"ensure": "absent",
				},
			},
		}
		mustExecute(t, tgt, "winget-absent", "winget_package", absentParams, ExecutionOptions{}, false, StatusChanged)
		if isWingetPackageInstalledOracle(t, runner, pkgID) {
			t.Fatal("independent oracle: expected package to be absent after removal")
		}

		// Idempotent absent — re-check returns OK (already absent)
		mustExecute(t, tgt, "winget-absent-idemp", "winget_package", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// isWingetAvailable checks whether winget.exe is available on the remote target.
func isWingetAvailable(ctx context.Context, tgt PowerShellRunner) bool {
	out, err := tgt.RunPowerShell(ctx,
		`if (Get-Command winget.exe -ErrorAction SilentlyContinue) { Write-Output 'true' } else { Write-Output 'false' }`)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// isWingetPackageInstalledOracle is an independent PowerShell oracle that runs
// winget list for the given package ID and returns true if the package is
// currently installed. This is a truthful assertion source separate from the
// module's own Check/Apply logic.
func isWingetPackageInstalledOracle(t *testing.T, tgt PowerShellRunner, pkgID string) bool {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
		`$result = & winget.exe list --id "%s" --exact --accept-source-agreements --disable-interactivity 2>&1; if ($LASTEXITCODE -eq 0) { Write-Output 'present' } else { Write-Output 'absent' }`,
		pkgID,
	))
	if err != nil {
		t.Fatalf("winget oracle script failed: %v", err)
	}
	return strings.TrimSpace(out) == "present"
}
