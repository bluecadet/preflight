package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWinRMIntegration_WingetPackage exercises the winget_package module
// against a real Windows endpoint over WinRM. It installs a small test
// package via winget, asserts correctness via an independent winget list
// read, verifies idempotency, and removes the package in cleanup.
//
// The test uses 7zip.7zip from the winget source as the fixture because it
// is small (~1.5 MB), benign, stable, and available on all Windows editions
// that ship with winget. If winget is not available on the target the test
// skips gracefully.
func TestWinRMIntegration_WingetPackage(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The test package. 7-Zip is small, stable, and universally available
	// via the winget source on all Windows editions that ship with winget.
	pkgID := "7zip.7zip"
	pkgSource := "winget"

	// ---- Cleanup: ensure the test package is uninstalled ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
			pkgID,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 0: Guard — winget availability ----
	if !isWingetAvailable(ctx, tgt) {
		t.Skip("winget.exe not found on target; skipping winget_package integration test")
	}

	// ---- Step 0.5: Ensure the package is not already installed ----
	// (in case a previous test run was interrupted)
	_, _ = tgt.RunPowerShell(ctx, fmt.Sprintf(
		`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
		pkgID,
	))

	// ---- Step 1: Verify package not installed via independent oracle ----
	if isWingetPackageInstalledOracle(t, tgt, pkgID) {
		t.Fatal("independent oracle: expected package to be absent before test")
	}

	// ---- Step 2: Apply the winget_package module to install ----
	params := map[string]any{
		"packages": []any{
			map[string]any{
				"id":     pkgID,
				"source": pkgSource,
			},
		},
	}

	result, err := tgt.Execute(ctx, "winget-apply", "winget_package", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("winget_package apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 3: Verify via independent oracle ----
	if !isWingetPackageInstalledOracle(t, tgt, pkgID) {
		t.Fatal("independent oracle: expected package to be installed after apply")
	}

	// ---- Step 4: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "winget-recheck", "winget_package", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("winget_package re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "winget-reapply", "winget_package", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("winget_package re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Ensure absent removes the package ----
	absentParams := map[string]any{
		"packages": []any{
			map[string]any{
				"id":     pkgID,
				"ensure": "absent",
			},
		},
	}

	result, err = tgt.Execute(ctx, "winget-absent", "winget_package", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("winget_package absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 7: Verify oracle confirms absence ----
	if isWingetPackageInstalledOracle(t, tgt, pkgID) {
		t.Fatal("independent oracle: expected package to be absent after removal")
	}

	// ---- Step 8: Ensure absent is idempotent ----
	result, err = tgt.Execute(ctx, "winget-absent-idempotent", "winget_package", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package absent idempotent: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("winget_package absent idempotent: expected StatusOK, got %q: %s", result.Status, result.Message)
	}
}

// isWingetAvailable checks whether winget.exe is available on the remote target.
func isWingetAvailable(ctx context.Context, tgt *WinRMTarget) bool {
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
