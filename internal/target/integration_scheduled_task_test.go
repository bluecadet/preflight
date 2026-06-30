package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWinRMIntegration_ScheduledTask exercises the scheduled_task module against a real
// Windows endpoint over WinRM. It is gated by PREFLIGHT_TEST_WINRM and the
// sacrificial sentinel, so it is inert on CI and on any dev machine without
// a configured VM.
//
// The test uses an independent PowerShell oracle (Get-ScheduledTask) to assert
// correctness rather than relying on the module's own Check().
func TestWinRMIntegration_ScheduledTask(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

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
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`Unregister-ScheduledTask -TaskPath "%s" -TaskName "%s" -Confirm:$false -ErrorAction SilentlyContinue`,
			taskPath, taskName,
		))
		if err != nil {
			t.Logf("cleanup (unregister task): %v", err)
		}

		// Remove the PreflightTest task folder via COM.
		_, err = tgt.RunPowerShell(ctx, `
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

	// ---- Step 1: Apply - create a scheduled task ----
	params := map[string]any{
		"path":      taskPath,
		"name":      taskName,
		"execute":   execute,
		"arguments": arguments,
		"trigger":   "startup",
	}

	result, err := tgt.Execute(ctx, "scheduled-task-apply", "scheduled_task", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("scheduled_task apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("scheduled_task apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	got := readScheduledTaskOracle(t, tgt, taskPath, taskName)
	expected := fmt.Sprintf("exists|%s|%s|%s|%s", taskName, taskPath, execute, arguments)
	if got != expected {
		t.Fatalf("independent oracle: expected %q, got %q", expected, got)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "scheduled-task-recheck", "scheduled_task", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("scheduled_task re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("scheduled_task re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "scheduled-task-reapply", "scheduled_task", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("scheduled_task re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("scheduled_task re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Ensure absent removes the task ----
	absentParams := map[string]any{
		"path":   taskPath,
		"name":   taskName,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "scheduled-task-absent", "scheduled_task", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("scheduled_task absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("scheduled_task absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Verify via oracle that task is gone
	got = readScheduledTaskOracle(t, tgt, taskPath, taskName)
	if got != "absent" {
		t.Fatalf("independent oracle: expected task to be absent, got %q", got)
	}
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
func readScheduledTaskOracle(t *testing.T, tgt *WinRMTarget, taskPath, taskName string) string {
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
