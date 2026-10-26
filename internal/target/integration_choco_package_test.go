//go:build integration

package target

import (
	"context"
	"strings"
	"testing"
)

// chocoExeExpr resolves choco.exe the same way the choco_package module does.
const chocoExeExpr = `(Join-Path ([Environment]::GetEnvironmentVariable('ChocolateyInstall', 'Machine')) 'bin\choco.exe')`

// TestIntegration_ChocoPackage exercises the choco_package module against a
// real Windows endpoint over every configured transport. Coverage per
// transport: present, idempotent, dry-run, version pin, drift, absent.
//
// The fixture is the jq package: small, benign, and published in several
// versions. The test skips when Chocolatey is not installed on the target.
func TestIntegration_ChocoPackage(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()
		const pkgID = "jq"
		uninstall := `& ` + chocoExeExpr + ` uninstall jq -y --no-progress 2>&1 | Out-Null`

		if !isChocoAvailable(ctx, runner) {
			t.Skip("choco.exe not found on target; skipping choco_package integration test")
		}
		t.Cleanup(func() {
			if _, err := runner.RunPowerShell(ctx, uninstall); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})
		_, _ = runner.RunPowerShell(ctx, uninstall)
		if chocoInstalledVersionOracle(t, runner, pkgID) != "" {
			t.Fatal("independent oracle: expected package to be absent before test")
		}

		present := map[string]any{"packages": []any{map[string]any{"id": pkgID}}}
		absent := map[string]any{"packages": []any{map[string]any{"id": pkgID, "ensure": "absent"}}}
		pinned := map[string]any{"packages": []any{map[string]any{"id": pkgID, "version": "1.6"}}}

		// present, then idempotent
		mustExecute(t, tgt, "choco-present", "choco_package", present, false, StatusChanged)
		if chocoInstalledVersionOracle(t, runner, pkgID) == "" {
			t.Fatal("independent oracle: expected package to be installed after apply")
		}
		mustExecute(t, tgt, "choco-idemp-check", "choco_package", present, true, StatusOK)
		mustExecute(t, tgt, "choco-idemp-apply", "choco_package", present, false, StatusOK)

		// dry-run must not mutate
		mustExecute(t, tgt, "choco-dryrun", "choco_package", absent, true, StatusChanged)
		if chocoInstalledVersionOracle(t, runner, pkgID) == "" {
			t.Fatal("independent oracle: expected package to remain installed after dry-run")
		}

		// version pin downgrades; 1.6 matches the installed 1.6.0
		mustExecute(t, tgt, "choco-pin", "choco_package", pinned, false, StatusChanged)
		if got := chocoInstalledVersionOracle(t, runner, pkgID); got != "1.6.0" {
			t.Fatalf("independent oracle: expected jq 1.6.0 after pin, got %q", got)
		}
		mustExecute(t, tgt, "choco-pin-idemp", "choco_package", pinned, false, StatusOK)

		// drift: removed behind the module's back, converges back
		if _, err := runner.RunPowerShell(ctx, uninstall); err != nil {
			t.Fatalf("drift setup: uninstall failed: %v", err)
		}
		mustExecute(t, tgt, "choco-drift-check", "choco_package", present, true, StatusChanged)
		mustExecute(t, tgt, "choco-drift-apply", "choco_package", present, false, StatusChanged)
		if chocoInstalledVersionOracle(t, runner, pkgID) == "" {
			t.Fatal("independent oracle: expected package to be re-installed after drift convergence")
		}

		// absent, then idempotent
		mustExecute(t, tgt, "choco-absent", "choco_package", absent, false, StatusChanged)
		if chocoInstalledVersionOracle(t, runner, pkgID) != "" {
			t.Fatal("independent oracle: expected package to be absent after removal")
		}
		mustExecute(t, tgt, "choco-absent-idemp", "choco_package", absent, false, StatusOK)
	})
}

func isChocoAvailable(ctx context.Context, tgt PowerShellRunner) bool {
	out, err := tgt.RunPowerShell(ctx, `if ([Environment]::GetEnvironmentVariable('ChocolateyInstall', 'Machine') -and (Test-Path -LiteralPath `+chocoExeExpr+`)) { 'true' } else { 'false' }`)
	return err == nil && strings.TrimSpace(out) == "true"
}

// chocoInstalledVersionOracle returns the installed version of pkgID, or ""
// when it is not installed, read independently of the module's scripts.
func chocoInstalledVersionOracle(t *testing.T, tgt PowerShellRunner, pkgID string) string {
	t.Helper()
	out, err := tgt.RunPowerShell(context.Background(),
		`& `+chocoExeExpr+` list --limit-output | Where-Object { $_ -like '`+pkgID+`|*' } | ForEach-Object { $_.Split('|')[1] }`)
	if err != nil {
		t.Fatalf("choco oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}
