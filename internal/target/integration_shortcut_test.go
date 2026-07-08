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

// TestIntegration_Shortcut exercises the shortcut (.LNK creation) module against
// a real Windows endpoint over every configured transport (WinRM and/or
// SSH-to-Windows). Each transport gets its own subtest and is independently
// skipped when its env vars are unset.
//
// Coverage template (per transport):
//   - present:     initial apply (create shortcut), verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift:       target/args change converges back to desired state
//   - absent:      shortcut removal, idempotent re-check
//
// The test uses an independent PowerShell oracle (WScript.Shell) to assert
// correctness of TargetPath and Arguments rather than relying on the module's
// own Check().
func TestIntegration_Shortcut(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// The namespace under which all test shortcuts are created. The run
		// ID suffix prevents collisions when multiple `go test` processes
		// share the same VM.
		nsDir := `%TEMP%\PreflightTest\ShortcutTest-` + testRunID()[:12]
		lnkPath := nsDir + `\test.lnk`
		targetExe := `C:\Windows\System32\notepad.exe`
		targetArgs := `/A readme.txt`

		// ---- Cleanup: remove the entire test namespace ----
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Remove-Item -LiteralPath ([System.Environment]::ExpandEnvironmentVariables("%s")) -Recurse -Force -ErrorAction SilentlyContinue`,
				nsDir,
			))
			if err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		desiredParams := map[string]any{
			"destination": lnkPath,
			"target":      targetExe,
			"args":        targetArgs,
			"ensure":      "present",
		}

		// ================================================================
		// Branch: present — initial apply creates the shortcut
		// ================================================================
		mustExecute(t, tgt, "shortcut-present", "shortcut", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Verify via independent oracle
		mustMatchShortcutOracle(t, runner, lnkPath, shortcutOracleResult{
			Present:    true,
			TargetPath: targetExe,
			Arguments:  targetArgs,
		})

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "shortcut-idemp-check", "shortcut", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "shortcut-idemp-apply", "shortcut", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustMatchShortcutOracle(t, runner, lnkPath, shortcutOracleResult{
			Present:    true,
			TargetPath: targetExe,
			Arguments:  targetArgs,
		})

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		dryRunTarget := `C:\Windows\System32\calc.exe`
		dryRunArgs := `/DryRun`
		dryRunParams := map[string]any{
			"destination": lnkPath,
			"target":      dryRunTarget,
			"args":        dryRunArgs,
			"ensure":      "present",
		}
		mustExecute(t, tgt, "shortcut-dryrun", "shortcut", dryRunParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms the shortcut was NOT changed by the dry run
		mustMatchShortcutOracle(t, runner, lnkPath, shortcutOracleResult{
			Present:    true,
			TargetPath: targetExe,
			Arguments:  targetArgs,
		})

		// ================================================================
		// Branch: drift — mutate behind the module's back, then verify
		// that Check detects the change and Apply converges back
		// ================================================================
		driftTarget := `C:\Windows\System32\calc.exe`
		driftArgs := `/Drifted`
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$path = [System.Environment]::ExpandEnvironmentVariables("%s")
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($path)
$shortcut.TargetPath = "%s"
$shortcut.Arguments = "%s"
$shortcut.Save()
`, lnkPath, driftTarget, driftArgs))
		if err != nil {
			t.Fatalf("drift setup: PowerShell drift mutation failed: %v", err)
		}
		// Oracle confirms the shortcut was mutated
		mustMatchShortcutOracle(t, runner, lnkPath, shortcutOracleResult{
			Present:    true,
			TargetPath: driftTarget,
			Arguments:  driftArgs,
		})

		// Check detects the drift (NeedsChange = true → StatusChanged)
		mustExecute(t, tgt, "shortcut-drift-check", "shortcut", desiredParams, ExecutionOptions{}, true, StatusChanged)

		// Apply converges back to the desired state
		mustExecute(t, tgt, "shortcut-drift-apply", "shortcut", desiredParams, ExecutionOptions{}, false, StatusChanged)
		mustMatchShortcutOracle(t, runner, lnkPath, shortcutOracleResult{
			Present:    true,
			TargetPath: targetExe,
			Arguments:  targetArgs,
		})

		// Confirm idempotence after convergence
		mustExecute(t, tgt, "shortcut-drift-idemp", "shortcut", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: absent — remove the shortcut, verify via oracle,
		// then confirm idempotent re-check
		// ================================================================
		absentParams := map[string]any{
			"destination": lnkPath,
			"ensure":      "absent",
		}
		mustExecute(t, tgt, "shortcut-absent", "shortcut", absentParams, ExecutionOptions{}, false, StatusChanged)

		// Verify via oracle that shortcut is gone
		mustMatchShortcutOracle(t, runner, lnkPath, shortcutOracleResult{
			Present: false,
		})

		// Idempotent absent — re-check returns OK (already absent)
		mustExecute(t, tgt, "shortcut-absent-idemp", "shortcut", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// mustMatchShortcutOracle asserts that the independent WScript.Shell oracle
// returns the expected shortcut properties for the given .lnk path. It fails
// the test if any field does not match.
func mustMatchShortcutOracle(t *testing.T, runner PowerShellRunner, lnkPath string, expected shortcutOracleResult) {
	t.Helper()
	got := readShortcutOracle(t, runner, lnkPath)
	if got.Present != expected.Present {
		t.Fatalf("independent oracle: expected Present=%v, got %v (path=%q)", expected.Present, got.Present, lnkPath)
	}
	if expected.Present {
		if got.TargetPath != expected.TargetPath {
			t.Fatalf("independent oracle: expected TargetPath=%q, got %q (path=%q)", expected.TargetPath, got.TargetPath, lnkPath)
		}
		if got.Arguments != expected.Arguments {
			t.Fatalf("independent oracle: expected Arguments=%q, got %q (path=%q)", expected.Arguments, got.Arguments, lnkPath)
		}
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