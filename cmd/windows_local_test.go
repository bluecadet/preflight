package cmd

import (
	"runtime"
	"testing"
)

// requireWindowsLocal skips tests that run the local target, which is only
// supported on Windows controllers.
func requireWindowsLocal(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("local target is Windows-only")
	}
}
