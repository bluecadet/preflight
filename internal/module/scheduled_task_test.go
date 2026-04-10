//go:build windows

package module

import (
	"context"
	"strings"
	"testing"
)

// TestScheduledTaskModule_NoCmdExecWrapper verifies that the scheduled_task
// Apply script does not wrap the command with "cmd.exe /c", which would expose
// a shell command injection vector when command contains metacharacters.
func TestScheduledTaskModule_NoCmdExecWrapper(t *testing.T) {
	var capturedScript string
	orig := windowsCombinedOutput
	t.Cleanup(func() { windowsCombinedOutput = orig })

	windowsCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			if strings.Contains(arg, "$params") {
				capturedScript = arg
				break
			}
		}
		// Simulate a successful task registration output.
		return []byte(""), nil
	}

	m := &ScheduledTaskModule{}
	_ = m.Apply(context.Background(), map[string]any{
		"name":    "test-task",
		"command": `C:\app\run.exe`,
		"trigger": "startup",
	})

	if capturedScript == "" {
		t.Skip("no script captured (PowerShell may not have been invoked in this test environment)")
	}

	// The script must not wrap the command with cmd.exe /c, which is a shell
	// command injection vector.
	if strings.Contains(capturedScript, "cmd.exe") {
		t.Errorf("script must not reference cmd.exe, got script fragment:\n%s", capturedScript)
	}
	if strings.Contains(capturedScript, "/c ") && strings.Contains(capturedScript, "command") {
		t.Errorf("script must not use '/c <command>' pattern:\n%s", capturedScript)
	}

	// The script must use -Execute with the command param directly.
	if !strings.Contains(capturedScript, "-Execute") {
		t.Errorf("script must use -Execute, got:\n%s", capturedScript)
	}
}

// TestScheduledTaskModule_CommandMetacharsDoNotInject verifies that a command
// string containing shell metacharacters does not result in a script that would
// execute them as operators when passed to New-ScheduledTaskAction -Execute.
func TestScheduledTaskModule_CommandMetacharsDoNotInject(t *testing.T) {
	var capturedScript string
	orig := windowsCombinedOutput
	t.Cleanup(func() { windowsCombinedOutput = orig })

	windowsCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			if strings.Contains(arg, "$params") {
				capturedScript = arg
				break
			}
		}
		return []byte(""), nil
	}

	maliciousCommand := `C:\app\run.exe & net user /add attacker P@ssw0rd`

	m := &ScheduledTaskModule{}
	_ = m.Apply(context.Background(), map[string]any{
		"name":    "test-task",
		"command": maliciousCommand,
		"trigger": "startup",
	})

	if capturedScript == "" {
		t.Skip("no script captured")
	}

	// The malicious operator literal must not appear unquoted in the script
	// in a position where it could be interpreted as a shell operator.
	// With -Execute ([string]$params.command), the value is dereferenced from
	// the JSON-encoded $params at runtime, so the script text itself only
	// contains the JSON-encoded form (which escapes the metacharacters).
	if strings.Contains(capturedScript, "& net user") {
		t.Errorf("metacharacter sequence appeared verbatim in script (injection risk):\n%s", capturedScript)
	}
}

func TestScheduledTaskModule_UsesPrincipalAndCreatesFolders(t *testing.T) {
	var capturedScript string
	orig := windowsCombinedOutput
	t.Cleanup(func() { windowsCombinedOutput = orig })

	windowsCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			if strings.Contains(arg, "$params") {
				capturedScript = arg
				break
			}
		}
		return []byte(""), nil
	}

	m := &ScheduledTaskModule{}
	_ = m.Apply(context.Background(), map[string]any{
		"name":      "test-task",
		"path":      `Preflight\Maintenance`,
		"command":   `C:\Windows\System32\shutdown.exe`,
		"trigger":   "daily",
		"start_at":  "04:30",
		"run_as":    "SYSTEM",
		"run_level": "highest",
	})

	if capturedScript == "" {
		t.Skip("no script captured")
	}
	if !strings.Contains(capturedScript, "Ensure-TaskFolder $path") {
		t.Fatalf("expected task folder creation helper, got:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "task '\" + $name + \"' was not registered in '\" + $path + \"'") {
		t.Fatalf("expected post-registration exact-folder verification, got:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "New-ScheduledTaskPrincipal") {
		t.Fatalf("expected explicit scheduled task principal, got:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "ServiceAccount") {
		t.Fatalf("expected service account logon type for SYSTEM tasks, got:\n%s", capturedScript)
	}
	if strings.Contains(capturedScript, "-User ([string]$params.run_as)") {
		t.Fatalf("expected task registration to avoid direct -User registration for run_as tasks, got:\n%s", capturedScript)
	}
}

func TestScheduledTaskModule_CheckUsesExactFolderLookup(t *testing.T) {
	var capturedScript string
	orig := windowsCombinedOutput
	t.Cleanup(func() { windowsCombinedOutput = orig })

	windowsCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			if strings.Contains(arg, "$params") {
				capturedScript = arg
				break
			}
		}
		return []byte("false"), nil
	}

	m := &ScheduledTaskModule{}
	_, _ = m.Check(context.Background(), map[string]any{
		"name":     "test-task",
		"path":     `Preflight`,
		"command":  `C:\Windows\System32\shutdown.exe`,
		"trigger":  "daily",
		"start_at": "04:30",
		"run_as":   "SYSTEM",
	})

	if capturedScript == "" {
		t.Skip("no script captured")
	}
	if !strings.Contains(capturedScript, "Get-TaskFromExactFolder $path $name") {
		t.Fatalf("expected exact-folder lookup helper usage, got:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "[string]$_.TaskPath -eq $path") {
		t.Fatalf("expected exact task-path filtering, got:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "$currentEnabled -ne $enabled") {
		t.Fatalf("expected enabled-state drift check, got:\n%s", capturedScript)
	}
}

func TestScheduledTaskModule_PresentTasksAreExplicitlyEnabled(t *testing.T) {
	var capturedScript string
	orig := windowsCombinedOutput
	t.Cleanup(func() { windowsCombinedOutput = orig })

	windowsCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			if strings.Contains(arg, "$params") {
				capturedScript = arg
				break
			}
		}
		return []byte(""), nil
	}

	m := &ScheduledTaskModule{}
	_ = m.Apply(context.Background(), map[string]any{
		"name":     "test-task",
		"path":     `Preflight`,
		"command":  `C:\Windows\System32\shutdown.exe`,
		"trigger":  "daily",
		"start_at": "04:30",
		"ensure":   "present",
		"enabled":  true,
	})

	if capturedScript == "" {
		t.Skip("no script captured")
	}
	if !strings.Contains(capturedScript, "Enable-ScheduledTask -TaskPath $path -TaskName $name | Out-Null") {
		t.Fatalf("expected present tasks to be explicitly enabled, got:\n%s", capturedScript)
	}
}
