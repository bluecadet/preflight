package target

import (
	"context"
	"strings"
	"testing"
)

// runOpsConformance is the shared ops-interface conformance suite every
// transport implementation of TargetOps (Exec/PutFile/GetFile/Info) must
// pass. It runs against the local target as a unit test and against each
// remote transport (SSH-POSIX in CI; SSH-Windows and WinRM in the dev-only
// suite) via the integration tests in ops_conformance_integration_test.go.
//
// newPath returns a fresh, unique path on the target's filesystem for the
// PutFile/GetFile round trip. The suite picks shell-appropriate commands from
// the RuntimeKind reported by Info, so the same assertions cover POSIX sh and
// Windows PowerShell transports.
//
// The suite asserts the contract, not transport performance: a non-zero exit is
// a result (not an error), Info carries the resolved runtime, and a
// PutFile/GetFile round trip is byte-exact.
func runOpsConformance(t *testing.T, ops TargetOps, newPath func() string) {
	t.Helper()
	ctx := context.Background()

	// Info: the enriched TargetInfo delivered to a plugin at initialize.
	info, err := ops.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.RuntimeKind == "" {
		t.Errorf("Info: RuntimeKind is empty")
	}
	if info.Transport == "" {
		t.Errorf("Info: Transport is empty")
	}

	// Exec (success): a trivial command in the native shell.
	okScript, okExpect, failScript := opsScripts(info.RuntimeKind)
	res, err := ops.Exec(ctx, okScript)
	if err != nil {
		t.Fatalf("Exec success: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("Exec success: exit code = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, okExpect) {
		t.Errorf("Exec success: stdout = %q, want it to contain %q", res.Stdout, okExpect)
	}

	// Exec (non-zero exit is a result, not an error). A plugin branches on exit
	// codes (e.g. `test -f`), so transports must surface them rather than
	// turning a non-zero exit into a Go error.
	res, err = ops.Exec(ctx, failScript)
	if err != nil {
		t.Fatalf("Exec fail: unexpected error (non-zero exit must be a result): %v", err)
	}
	if res.ExitCode == 0 {
		t.Errorf("Exec fail: exit code = 0, want non-zero")
	}

	// PutFile + GetFile round trip: bytes written must read back byte-exact,
	// including parent-directory creation.
	path := newPath()
	payload := []byte("ops-conformance-put-bytes")
	if err := ops.PutFile(ctx, path, payload); err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	got, err := ops.GetFile(ctx, path)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("GetFile round trip: got %q, want %q", got, payload)
	}
}

// opsScripts returns shell-appropriate scripts for the conformance suite given
// the target's resolved RuntimeKind. okScript is a trivial success; failScript
// exits non-zero without throwing, so the suite can assert the exit code is
// surfaced as a result rather than an error.
func opsScripts(kind RuntimeKind) (okScript, okExpect, failScript string) {
	if kind == RuntimeKindWindowsPowerShell {
		return "Write-Output pf-exec-ok", "pf-exec-ok", "exit 1"
	}
	return "printf pf-exec-ok", "pf-exec-ok", "false"
}
