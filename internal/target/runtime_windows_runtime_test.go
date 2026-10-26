package target

import (
	"context"
	"strings"
	"testing"
)

func TestApplyWindowsShellWrapsEnv(t *testing.T) {
	for name, workingDir := range map[string]string{"no working dir": "", "working dir": `C:\Exhibits`} {
		t.Run(name, func(t *testing.T) {
			backend := &recordingPowerShellBackend{output: "done"}
			_, err := applyWindowsShell(context.Background(), backend, map[string]any{
				"cmd":         "app.exe",
				"working_dir": workingDir,
				"env":         map[string]any{"MODE": "kiosk"},
			}, nil)
			if err != nil {
				t.Fatalf("applyWindowsShell returned error: %v", err)
			}
			script := backend.scripts[0]
			for _, want := range []string{"$__pf_env_name", "eyJNT0RFIjoia2lvc2sifQ==", "& $cmd @args"} {
				if !strings.Contains(script, want) {
					t.Fatalf("expected script to contain %q, got:\n%s", want, script)
				}
			}
		})
	}
}

func TestWindowsWaitPortOpenDoesNotAssignHostVariable(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "true"}
	met, err := windowsWaitCondition(context.Background(), backend, "port_open", "localhost:80")
	if err != nil {
		t.Fatalf("windowsWaitCondition returned error: %v", err)
	}
	if !met {
		t.Fatal("expected condition to be met")
	}
	// $Host is a read-only automatic variable; assigning it always throws.
	for line := range strings.SplitSeq(backend.scripts[0], "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "$host ") {
			t.Fatalf("script assigns read-only $Host: %q", line)
		}
	}
}
