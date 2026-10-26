package target

import (
	"context"
	"strings"
	"testing"
)

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
