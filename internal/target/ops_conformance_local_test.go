package target

import (
	"path/filepath"
	"testing"
)

// TestOpsConformance_Local runs the shared ops-interface conformance suite
// against the local target. On a POSIX controller (CI) this exercises the
// POSIX-sh path; the SSH-POSIX, SSH-Windows, and WinRM variants live in
// ops_conformance_integration_test.go behind the integration build tag.
func TestOpsConformance_Local(t *testing.T) {
	tgt := NewLocalTarget(nil)
	defer func() { _ = tgt.Close() }()
	dir := t.TempDir()
	runOpsConformance(t, tgt, func() string {
		return filepath.Join(dir, "pf-ops-conf")
	})
}
