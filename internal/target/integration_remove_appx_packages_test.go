package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWinRMIntegration_RemoveAppxPackages exercises the remove_appx_packages
// module against a real Windows endpoint over WinRM. It is gated by
// PREFLIGHT_TEST_WINRM and the sacrificial sentinel, so it is inert on CI
// and on any dev machine without a configured VM.
//
// The test finds a benign throwaway AppX package on the target (matching
// known safe patterns like VCLibs, UI.Xaml, .NET Native, WinJS), removes it
// via the module, and asserts correctness via an independent Get-AppxPackage
// oracle. In cleanup it makes a best-effort attempt to re-register the package.
func TestWinRMIntegration_RemoveAppxPackages(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// ---- Find a benign AppX fixture on the target ----
	fixtureName, fixtureFamilyName := findAppxFixture(t, tgt)
	if fixtureName == "" {
		t.Skip("no suitable AppX test fixture found on target; " +
			"target has no packages matching known-benign patterns (VCLibs, UI.Xaml, .NET Native, WinJS)")
	}
	t.Logf("using AppX fixture: Name=%s FamilyName=%s", fixtureName, fixtureFamilyName)

	// ---- Cleanup: best-effort re-registration of the removed package ----
	// Removing AppX packages is destructive. After the test completes we
	// attempt to re-register the package via its manifest in WindowsApps
	// (which persists after removal). If re-registration fails the VM state
	// is left altered, which is acceptable for the sacrificial target.
	t.Cleanup(func() {
		// First check if the package auto-restored.
		if appxPackageOracle(t, tgt, fixtureName) == "present" {
			return
		}
		// When we have the family name, try RegisterByFamilyName (available
		// on Windows 10 1809+). This re-adds the package from the cached
		// payload in ProgramFiles\WindowsApps without requiring the
		// original .appxbundle.
		if fixtureFamilyName != "" {
			_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
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

	// ---- Step 1: Verify fixture exists via independent oracle ----
	status := appxPackageOracle(t, tgt, fixtureName)
	if status != "present" {
		t.Fatalf("independent oracle: fixture package %q is %q before test (expected present)", fixtureName, status)
	}

	// ---- Step 2: Apply — remove the package ----
	params := map[string]any{
		"name":  fixtureName,
		"scope": "all_users",
	}

	result, err := tgt.Execute(ctx, "appx-remove", "remove_appx_packages", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("remove_appx_packages apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("remove_appx_packages apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 3: Verify removal via independent oracle ----
	status = appxPackageOracle(t, tgt, fixtureName)
	if status == "present" {
		// The module reports StatusChanged because Check saw the package and
		// the removal command ran, but Remove-AppxPackage -AllUsers swallows
		// failures as warnings. Probe the underlying error to tell an
		// environment limitation apart from a real module defect: all-users
		// AppX removal fails with 0x80073D19 ("a user was logged off") in a
		// non-interactive WinRM session. That is a session property, so skip.
		if reason := appxRemovalBlockedReason(t, tgt, fixtureName); reason != "" {
			t.Skipf("AppX all-users removal is unsupported over this WinRM session "+
				"(requires an interactive logon): %s", reason)
		}
		t.Fatal("independent oracle: package still present after removal (expected absent)")
	}
	t.Logf("oracle confirms package removed: %s", status)

	// ---- Step 4: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "appx-recheck", "remove_appx_packages", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("remove_appx_packages re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("remove_appx_packages re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "appx-reapply", "remove_appx_packages", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("remove_appx_packages re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("remove_appx_packages re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}
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
