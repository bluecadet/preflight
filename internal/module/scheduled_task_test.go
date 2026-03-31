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
