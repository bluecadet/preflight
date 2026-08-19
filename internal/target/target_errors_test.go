package target

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/bluecadet/preflight/internal/preflighterr"
)

// TestIsUnreachable_RealWinRMDialFailure drives an actual *WinRMTarget.Info
// call through the real production wrapping chain: clientFactory fails,
// clientForUse wraps it (wrapUnreachableWinRMError, Op "create client"),
// getOrCreatePSSession's failure is discarded by runPSWithFallback's
// fallback-to-per-invocation path, runPSPerInvocation hits clientFactory a
// second time and returns that TargetError as-is, and
// remoteWindowsTargetInfo wraps it again with Op "info" — which
// wrapTargetError's own "don't rewrap an existing *TargetError" rule turns
// into a no-op. This is the exact multi-layer path the schema-drift fix's
// classifier must survive; a hand-built single-level *TargetError (as an
// earlier version of this test used) would not have caught the real bug.
func TestIsUnreachable_RealWinRMDialFailure(t *testing.T) {
	tgt := NewWinRMTarget(WinRMConfig{Host: "unreachable-host"}, nil)
	dialErr := errors.New("dial tcp 10.0.0.9:5986: connect: connection refused")
	tgt.clientFactory = func(WinRMConfig) (winRMClient, error) {
		return nil, dialErr
	}

	_, err := tgt.Info(context.Background())
	if err == nil {
		t.Fatal("expected Info to fail when the WinRM client factory fails")
	}
	if !IsUnreachable(err) {
		t.Fatalf("IsUnreachable(err) = false, want true; err = %v", err)
	}
	if !errors.Is(err, dialErr) {
		t.Fatalf("expected the original dial error to remain in the chain: %v", err)
	}
}

// TestIsUnreachable_RealSSHDialFailure is the SSH analog: a failing
// runnerFactory (dial/handshake) drives a real *SSHTarget.Info call through
// clientRunner -> probePowerShellBinary/detectRuntime -> runtimeForUse.
func TestIsUnreachable_RealSSHDialFailure(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "unreachable-host"}, nil)
	dialErr := errors.New("dial tcp 10.0.0.9:22: connect: connection refused")
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return nil, dialErr
	}

	_, err := tgt.Info(context.Background())
	if err == nil {
		t.Fatal("expected Info to fail when the SSH runner factory fails")
	}
	if !IsUnreachable(err) {
		t.Fatalf("IsUnreachable(err) = false, want true; err = %v", err)
	}
	if !errors.Is(err, dialErr) {
		t.Fatalf("expected the original dial error to remain in the chain: %v", err)
	}
}

// TestIsUnreachable_RealWinRMRunPSTransportFailure exercises the other real
// WinRM path into a connection failure: client construction succeeds (no
// eager dial), the client does not support persistent shells so
// runPSWithFallback falls through to runPSPerInvocation, and the actual
// RPC call (RunPSWithContext) fails with a transport-level error — the
// realistic shape of "connection refused" for a library that dials lazily.
// Before this fix, this path's failure was wrapped under Op "powershell
// failed", which was not recognized as unreachable (it is shared with
// ordinary non-zero-exit script failures, so it could not simply be added
// wholesale) — exactly the gap the sentinel-based classifier closes.
func TestIsUnreachable_RealWinRMRunPSTransportFailure(t *testing.T) {
	connErr := errors.New("dial tcp 10.0.0.9:5986: connect: connection refused")
	tgt := NewWinRMTarget(WinRMConfig{Host: "unreachable-host"}, nil)
	tgt.clientFactory = func(WinRMConfig) (winRMClient, error) {
		return &fakeWinRMClient{
			runPS: func(context.Context, string) (string, string, int, error) {
				return "", "", 0, connErr
			},
		}, nil
	}

	_, err := tgt.Info(context.Background())
	if err == nil {
		t.Fatal("expected Info to fail when the RunPSWithContext RPC fails")
	}
	if !IsUnreachable(err) {
		t.Fatalf("IsUnreachable(err) = false, want true; err = %v", err)
	}
	if !errors.Is(err, connErr) {
		t.Fatalf("expected the original transport error to remain in the chain: %v", err)
	}
}

// TestIsUnreachable_SurvivesManualDoubleWrap directly exercises the
// wrap-order scenario the blocker was found in: a leaf connection failure
// wrapped via wrapUnreachable, then wrapped again by an outer, unrelated Op
// (simulating remoteWindowsTargetInfo's "info" wrap) via the plain
// wrapTargetError. IsUnreachable must still return true, and the original,
// more specific Op ("create client") must survive rather than being
// clobbered by the outer "info" — proving classification does not depend
// on which TargetError ends up outermost.
func TestIsUnreachable_SurvivesManualDoubleWrap(t *testing.T) {
	leaf := errors.New("dial tcp: connection refused")
	inner := wrapUnreachableWinRMError("create client", leaf)

	outer := wrapTargetError(TransportWinRM, "info", inner)

	if !IsUnreachable(outer) {
		t.Fatalf("IsUnreachable(outer) = false, want true; outer = %v", outer)
	}

	var targetErr *preflighterr.TargetError
	if !errors.As(outer, &targetErr) {
		t.Fatalf("expected *preflighterr.TargetError in chain, got %v", outer)
	}
	if targetErr.Op != "create client" {
		t.Fatalf("expected the inner leaf's Op to survive the outer wrap, got %q", targetErr.Op)
	}
}

// TestIsUnreachable_NonConnectionErrorsAreNotUnreachable guards against
// false positives: none of these represent a failure to reach the target
// at all, and must not classify as target_unreachable.
func TestIsUnreachable_NonConnectionErrorsAreNotUnreachable(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "plain error, no TargetError at all",
			err:  errors.New("boom"),
		},
		{
			name: "SSH runtime detection failure (connected fine)",
			err:  wrapSSHTargetError("detect runtime", errors.New("unable to detect a supported remote runtime")),
		},
		{
			name: "SSH exec failure over a working connection",
			err:  wrapSSHTargetError("exec", errors.New("command not found")),
		},
		{
			name: "WinRM script exited non-zero (transport worked fine)",
			err:  wrapWinRMTargetError("powershell failed", fmt.Errorf("exited with code 1: boom")),
		},
		{
			name: "WinRM script encode failure (local, not network)",
			err:  wrapWinRMTargetError("powershell failed", errors.New("cannot encode script")),
		},
		{
			name: "wrapped once more with an unrelated Op",
			err:  wrapTargetError(TransportWinRM, "info", wrapWinRMTargetError("powershell failed", fmt.Errorf("exited with code 1: boom"))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if IsUnreachable(tc.err) {
				t.Fatalf("IsUnreachable(%v) = true, want false", tc.err)
			}
		})
	}
}
