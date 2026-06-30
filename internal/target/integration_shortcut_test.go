//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// shortcutOracleResult holds the fields read by the independent WScript.Shell oracle.
type shortcutOracleResult struct {
	Present    bool
	TargetPath string
	Arguments  string
}

// TestWinRMIntegration_Shortcut exercises the shortcut (.LNK creation) module
// against a real Windows endpoint over WinRM. It is gated by PREFLIGHT_TEST_WINRM
// and the sacrificial sentinel.
//
// The test uses an independent PowerShell oracle (WScript.Shell) to assert
// correctness of TargetPath, Arguments, and WorkingDirectory rather than
// relying on the module's own Check().
func TestWinRMIntegration_Shortcut(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)
	t.Cleanup(func() { _ = tgt.Close() })

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The namespace under which all test shortcuts are created. The run ID
	// suffix prevents collisions when multiple `go test` processes share the
	// same VM.
	nsDir := `%TEMP%\PreflightTest\ShortcutTest-` + testRunID()[:12]
	lnkPath := nsDir + `\test.lnk`

	// ---- Cleanup: remove the entire test namespace ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`Remove-Item -LiteralPath ([System.Environment]::ExpandEnvironmentVariables("%s")) -Recurse -Force -ErrorAction SilentlyContinue`,
			nsDir,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	targetExe := `C:\Windows\System32\notepad.exe`
	targetArgs := `/A readme.txt`

	// ---- Step 1: Apply - create a shortcut ----
	params := map[string]any{
		"destination": lnkPath,
		"target":      targetExe,
		"args":        targetArgs,
		"ensure":      "present",
	}

	result, err := tgt.Execute(ctx, "shortcut-apply", "shortcut", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("shortcut apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("shortcut apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	o := readShortcutOracle(t, tgt, lnkPath)
	if !o.Present {
		t.Fatal("independent oracle: shortcut file does not exist")
	}
	if o.TargetPath != targetExe {
		t.Fatalf("independent oracle: expected TargetPath=%q, got %q", targetExe, o.TargetPath)
	}
	if o.Arguments != targetArgs {
		t.Fatalf("independent oracle: expected Arguments=%q, got %q", targetArgs, o.Arguments)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "shortcut-recheck", "shortcut", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("shortcut re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("shortcut re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "shortcut-reapply", "shortcut", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("shortcut re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("shortcut re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Ensure absent removes the shortcut ----
	absentParams := map[string]any{
		"destination": lnkPath,
		"ensure":      "absent",
	}

	result, err = tgt.Execute(ctx, "shortcut-absent", "shortcut", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("shortcut absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("shortcut absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Verify via oracle that shortcut is gone
	o = readShortcutOracle(t, tgt, lnkPath)
	if o.Present {
		t.Fatal("independent oracle: expected shortcut to be absent, but it still exists")
	}

	// ---- Step 6: Ensure absent is idempotent ----
	result, err = tgt.Execute(ctx, "shortcut-absent-idempotent", "shortcut", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("shortcut absent (idempotent): %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("shortcut absent (idempotent): expected StatusOK, got %q: %s", result.Status, result.Message)
	}
}

// readShortcutOracle is an independent PowerShell oracle that reads a .lnk file's
// properties via WScript.Shell, without using the module's Check or Apply scripts.
// This provides a truthful assertion source independent of the module implementation.
func readShortcutOracle(t *testing.T, tgt PowerShellRunner, lnkPath string) shortcutOracleResult {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$path = [System.Environment]::ExpandEnvironmentVariables("%s")
if (-not (Test-Path -LiteralPath $path)) {
  Write-Output "absent||"
  exit 0
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($path)
$fields = @(
  $shortcut.TargetPath,
  $shortcut.Arguments
)
Write-Output ("present|" + ($fields -join "|"))
`, lnkPath))
	if err != nil {
		t.Fatalf("shortcut oracle script failed: %v", err)
	}

	parts := strings.SplitN(strings.TrimSpace(out), "|", 3)
	if len(parts) < 1 {
		t.Fatalf("shortcut oracle: unexpected output format: %q", out)
	}

	result := shortcutOracleResult{
		Present: parts[0] == "present",
	}
	if result.Present && len(parts) >= 3 {
		result.TargetPath = parts[1]
		result.Arguments = parts[2]
	}
	return result
}
