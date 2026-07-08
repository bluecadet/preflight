//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_ScheduledTask exercises the scheduled_task module against a real
// Windows endpoint over every configured transport (WinRM and/or SSH-to-Windows).
// Each transport gets its own subtest and is independently skipped when its
// env vars are unset.
//
// Uses an independent PowerShell oracle (Get-ScheduledTask) to assert correctness
// rather than relying on the module's own Check().
//
// Coverage template (per transport):
//   - present:     initial apply creates the task, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift:       modify execute/arguments behind module's back, check detects,
//                  apply converges back
//   - absent:      remove the task, verify via oracle
//
// MUST run exclusively — never co-scheduled with other live-VM tests
// (per ADR-0015).
func TestIntegration_ScheduledTask(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// Test task lives under \PreflightTest\ so cleanup is scoped and the
		// sentinel at IsSacrificial is never touched. The run ID suffix prevents
		// collisions when multiple `go test` processes share the same VM.
		taskPath := `\PreflightTest\`
		taskName := fmt.Sprintf("pf-test-%s", testRunID()[:12])
		execute := `powershell.exe`
		arguments := `-NoProfile -NonInteractive -Command exit 0`

		// ---- Cleanup: remove the test task and its parent folder ----
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Unregister-ScheduledTask -TaskPath "%s" -TaskName "%s" -Confirm:$false -ErrorAction SilentlyContinue`,
				taskPath, taskName,
			))
			if err != nil {
				t.Logf("cleanup (unregister task): %v", err)
			}

			// Remove the PreflightTest task folder via COM.
			_, err = runner.RunPowerShell(ctx, `
$service = New-Object -ComObject 'Schedule.Service'
$service.Connect()
try {
  $parent = $service.GetFolder('\')
  $parent.DeleteFolder('PreflightTest', $null)
} catch { }
`)
			if err != nil {
				t.Logf("cleanup (delete folder): %v", err)
			}
		})

		desiredParams := map[string]any{
			"path":      taskPath,
			"name":      taskName,
			"execute":   execute,
			"arguments": arguments,
			"trigger":   "startup",
			"ensure":    "present",
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation (task does not exist yet)
		// ================================================================
		mustExecute(t, tgt, "scheduled-task-dryrun", "scheduled_task", desiredParams, ExecutionOptions{}, true, StatusChanged)
		got := readScheduledTaskOracle(t, runner, taskPath, taskName)
		if got != "absent" {
			t.Fatalf("dry-run: oracle expected task to be absent, got %q", got)
		}
		t.Log("dry-run: oracle confirms task was not created")

		// ================================================================
		// Branch: present — initial apply creates the scheduled task
		// ================================================================
		mustExecute(t, tgt, "scheduled-task-apply", "scheduled_task", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Verify via independent oracle
		expected := fmt.Sprintf("exists|%s|%s|%s|%s", taskName, taskPath, execute, arguments)
		got = readScheduledTaskOracle(t, runner, taskPath, taskName)
		if got != expected {
			t.Fatalf("independent oracle: expected %q, got %q", expected, got)
		}

		// ================================================================
		// Branch: idempotent — re-check and re-apply both return StatusOK
		// ================================================================
		mustExecute(t, tgt, "scheduled-task-idemp-check", "scheduled_task", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "scheduled-task-idemp-apply", "scheduled_task", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: drift — modify execute/arguments behind the module's
		// back, then verify Check detects the change and Apply converges
		// back to the desired state
		// ================================================================
		driftExecute := `cmd.exe`
		driftArguments := `/c echo drifted`
		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$tp = "%s"
$tn = "%s"
$task = Get-ScheduledTask -TaskPath $tp -TaskName $tn -ErrorAction Stop
$action = $task.Actions | Select-Object -First 1
$action.Execute = '%s'
$action.Arguments = '%s'
$task | Set-ScheduledTask -ErrorAction Stop | Out-Null
`, taskPath, taskName, driftExecute, driftArguments))
		if err != nil {
			t.Fatalf("drift setup: failed to modify task: %v", err)
		}

		// Oracle confirms the drift took effect
		driftedExpected := fmt.Sprintf("exists|%s|%s|%s|%s", taskName, taskPath, driftExecute, driftArguments)
		got = readScheduledTaskOracle(t, runner, taskPath, taskName)
		if got != driftedExpected {
			t.Fatalf("drift oracle: expected %q, got %q", driftedExpected, got)
		}

		// Check detects the drift (NeedsChange = true → StatusChanged)
		mustExecute(t, tgt, "scheduled-task-drift-check", "scheduled_task", desiredParams, ExecutionOptions{}, true, StatusChanged)

		// Apply converges back to the desired state
		mustExecute(t, tgt, "scheduled-task-drift-apply", "scheduled_task", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// Oracle confirms convergence
		got = readScheduledTaskOracle(t, runner, taskPath, taskName)
		if got != expected {
			t.Fatalf("drift convergence oracle: expected %q, got %q", expected, got)
		}

		// Confirm idempotence after convergence
		mustExecute(t, tgt, "scheduled-task-drift-idemp", "scheduled_task", desiredParams, ExecutionOptions{}, false, StatusOK)

		// ================================================================
		// Branch: absent — remove the scheduled task entirely
		// ================================================================
		absentParams := map[string]any{
			"path":   taskPath,
			"name":   taskName,
			"ensure": "absent",
		}

		mustExecute(t, tgt, "scheduled-task-absent", "scheduled_task", absentParams, ExecutionOptions{}, false, StatusChanged)

		// Verify via oracle that task is gone
		got = readScheduledTaskOracle(t, runner, taskPath, taskName)
		if got != "absent" {
			t.Fatalf("independent oracle: expected task to be absent, got %q", got)
		}

		// Idempotent absent — re-check returns OK (already absent)
		mustExecute(t, tgt, "scheduled-task-absent-idemp", "scheduled_task", absentParams, ExecutionOptions{}, false, StatusOK)
	})
}

// readScheduledTaskOracle is an independent PowerShell oracle that reads a
// scheduled task's state via Get-ScheduledTask. It is written separately from
// the module's own Check script to serve as a truthful assertion source.
//
// Returns "exists|taskName|taskPath|execute|arguments" when the task exists,
// or "absent" when it does not.
//
// Task path is injected into a PowerShell double-quoted string. PowerShell
// treats backslash as a literal character inside double quotes, so no
// additional escaping is needed.
func readScheduledTaskOracle(t *testing.T, tgt PowerShellRunner, taskPath, taskName string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$tp = "%s"
$tn = "%s"
$task = Get-ScheduledTask -TaskPath $tp -TaskName $tn -ErrorAction SilentlyContinue |
  Where-Object { [string]$_.TaskPath -eq $tp -and [string]$_.TaskName -eq $tn } |
  Select-Object -First 1
if ($null -eq $task) {
  Write-Output 'absent'
  exit 0
}
$action = $task.Actions | Select-Object -First 1
$execute = if ($action.Execute) { $action.Execute } else { '' }
$arguments = if ($action.Arguments) { $action.Arguments } else { '' }
Write-Output ("exists|" + $task.TaskName + "|" + $task.TaskPath + "|" + $execute + "|" + $arguments)
`, taskPath, taskName))
	if err != nil {
		t.Fatalf("oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}
