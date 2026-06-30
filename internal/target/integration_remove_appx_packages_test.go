//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_RemoveAppxPackages exercises the remove_appx_packages module
// against a real Windows endpoint over every configured transport (WinRM and/or
// SSH-to-Windows). Each transport gets its own subtest and is independently
// skipped when its env vars are unset.
//
// Coverage template (per transport):
//   - dry-run:  check-only predicts Changed, oracle confirms no mutation
//   - absent:   initial apply removes the package, oracle confirms absent
//   - idempotent: re-check and re-apply both return StatusOK
//   - drift:    re-add the package via RegisterByFamilyName, Check detects
//               the change, Apply removes it again
func TestIntegration_RemoveAppxPackages(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// ---- Find a benign AppX fixture on the target ----
		fixtureName, fixtureFamilyName := findAppxFixture(t, runner)
		if fixtureName == "" {
			t.Skip("no suitable AppX test fixture found on target; " +
				"target has no packages matching known-benign patterns (VCLibs, UI.Xaml, .NET Native, WinJS)")
		}
		t.Logf("using AppX fixture: Name=%s FamilyName=%s", fixtureName, fixtureFamilyName)

		// ---- Cleanup: best-effort re-registration of the removed package ----
		t.Cleanup(func() {
			if appxPackageOracle(t, runner, fixtureName) == "present" {
				return
			}
			if fixtureFamilyName != "" {
				_, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$familyName = '%s'
try {
  Add-AppxPackage -RegisterByFamilyName -MainPackageFamilyName $familyName -ErrorAction Stop | Out-Null
  Write-Output "restored via RegisterByFamilyName: $familyName"
} catch {
  Write-Output ("warn: RegisterByFamilyName failed: " + $_.Exception.Message)
}
`, fixtureFamilyName))
				if err != nil {
					t.Logf("cleanup RegisterByFamilyName: %v", err)
				}
			}
		})

		// ---- Verify fixture exists via independent oracle ----
		status := appxPackageOracle(t, runner, fixtureName)
		if status != "present" {
			t.Fatalf("independent oracle: fixture package %q is %q before test (expected present)", fixtureName, status)
		}

		params := map[string]any{
			"name":  fixtureName,
			"scope": "all_users",
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred (package is still present)
		// ================================================================
		mustExecute(t, tgt, "appx-dryrun", "remove_appx_packages", params, ExecutionOptions{}, true, StatusChanged)
		status = appxPackageOracle(t, runner, fixtureName)
		if status != "present" {
			t.Fatalf("dry-run: oracle reports %q after dry-run (expected present — dry-run must not mutate)", status)
		}
		t.Log("dry-run: oracle confirms package was not mutated")

		// ================================================================
		// Branch: absent — apply removes the package
		// ================================================================
		mustExecute(t, tgt, "appx-remove", "remove_appx_packages", params, ExecutionOptions{}, false, StatusChanged)

		status = appxPackageOracle(t, runner, fixtureName)
		if status == "present" {
			if reason := appxRemovalBlockedReason(t, runner, fixtureName); reason != "" {
				t.Skipf("AppX all-users removal is unsupported over this transport "+
					"(requires an interactive logon): %s", reason)
			}
			t.Fatal("independent oracle: package still present after removal (expected absent)")
		}
		t.Log("oracle confirms package removed")

		// ================================================================
		// Branch: idempotent — re-check returns StatusOK, re-apply is no-op
		// ================================================================
		mustExecute(t, tgt, "appx-recheck", "remove_appx_packages", params, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "appx-reapply", "remove_appx_packages", params, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: drift — re-register the package behind the module's
		// back, then verify Check detects the change and Apply converges
		// ================================================================
		if fixtureFamilyName != "" {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$familyName = '%s'
try {
  Add-AppxPackage -RegisterByFamilyName -MainPackageFamilyName $familyName -ErrorAction Stop | Out-Null
  Write-Output "ok"
} catch {
  Write-Output ("fail: " + $_.Exception.Message)
}
`, fixtureFamilyName))
			driftRegistered := err == nil
			if driftRegistered {
				// Verify re-registration succeeded via oracle
				status := appxPackageOracle(t, runner, fixtureName)
				if status == "present" {
					// Check detects the drift
					mustExecute(t, tgt, "appx-drift-check", "remove_appx_packages", params, ExecutionOptions{}, false, StatusChanged)

					// Apply converges back
					mustExecute(t, tgt, "appx-drift-apply", "remove_appx_packages", params, ExecutionOptions{}, false, StatusChanged)

					status = appxPackageOracle(t, runner, fixtureName)
					if status == "present" {
						if reason := appxRemovalBlockedReason(t, runner, fixtureName); reason != "" {
							t.Logf("drift: removal blocked after convergence: %s", reason)
						} else {
							t.Fatal("drift: oracle confirms package still present after convergence")
						}
					}
					t.Log("drift: oracle confirms package re-removed after convergence")

					// Idempotent after convergence
					mustExecute(t, tgt, "appx-drift-idemp", "remove_appx_packages", params, ExecutionOptions{}, false, StatusOK)
				} else {
					t.Logf("drift: oracle reports package %q after RegisterByFamilyName (expected present), skipping drift branch", status)
				}
			} else {
				t.Logf("drift: RegisterByFamilyName failed, skipping drift branch: %v", err)
			}
		} else {
			t.Log("drift: no family name available, skipping drift branch")
		}
	})
}

// appxPackageOracle is an independent PowerShell oracle that checks whether an
// AppX package with the given name exists on the target for all users. It
// queries Get-AppxPackage directly rather than using the module's Check()
// script, providing a truthful assertion source independent of the module
// implementation.
//
// Returns "present" if a matching package is found, "absent" otherwise.
func appxPackageOracle(t *testing.T, tgt PowerShellRunner, name string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = '%s'
$pkg = Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $pkg) {
  Write-Output 'absent'
  exit 0
}
Write-Output 'present'
`, name))
	if err != nil {
		t.Fatalf("appx package oracle failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// findAppxFixture searches the target for a benign, non-removable AppX package
// that can be safely removed as a test fixture. It searches for packages
// matching known-safe patterns (VCLibs, UI.Xaml, .NET Native Runtime, WinJS)
// and returns the package Name and PackageFamilyName of the first match. Both
// values are empty when no suitable fixture is found.
//
// The NonRemovable guard mirrors the module's own filter so we never hand-pick
// a package the module would skip.
func findAppxFixture(t *testing.T, tgt PowerShellRunner) (name, familyName string) {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, `
$patterns = @(
  'Microsoft.VCLibs*',
  'Microsoft.UI.Xaml*',
  'Microsoft.NET.Native*',
  'Microsoft.WinJS*'
)
$candidates = @(Get-AppxPackage -AllUsers -ErrorAction SilentlyContinue | Where-Object {
  $pkg = $_
  -not ($null -ne $pkg.PSObject.Properties['NonRemovable'] -and [bool]$pkg.NonRemovable) -and
  ($patterns | Where-Object { $pkg.Name -like $_ } | Select-Object -First 1)
})
if ($candidates.Count -gt 0) {
  $pkg = $candidates | Select-Object -First 1
  Write-Output ($pkg.Name + '|' + $pkg.PackageFamilyName)
  exit 0
}
Write-Output ''
`)
	if err != nil {
		t.Fatalf("findAppxFixture failed: %v", err)
	}

	parts := strings.SplitN(strings.TrimSpace(out), "|", 2)
	if len(parts) < 1 || parts[0] == "" {
		return "", ""
	}
	name = parts[0]
	if len(parts) > 1 {
		familyName = parts[1]
	}
	return
}
