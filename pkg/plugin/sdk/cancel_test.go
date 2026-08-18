package sdk

import (
	"context"
	"errors"
	"testing"
	"time"
)

// cancelAwareModule blocks in Check/Apply until the context it was handed is
// cancelled, then reports the error it observed. It is the instrument for the
// central claim of protocol v2: the ctx a plugin receives is a real
// cancellation signal, not a placeholder.
type cancelAwareModule struct {
	entered  chan struct{}
	observed chan error
}

func newCancelAwareModule() *cancelAwareModule {
	return &cancelAwareModule{
		entered:  make(chan struct{}),
		observed: make(chan error, 1),
	}
}

func (m *cancelAwareModule) Name() string    { return "cancel-mock" }
func (m *cancelAwareModule) Version() string { return "1.0.0" }

func (m *cancelAwareModule) block(ctx context.Context) error {
	close(m.entered)
	<-ctx.Done()
	m.observed <- ctx.Err()
	return ctx.Err()
}

func (m *cancelAwareModule) Check(ctx context.Context, _ map[string]any, _ Handle) (CheckResult, error) {
	return CheckResult{}, m.block(ctx)
}

func (m *cancelAwareModule) Apply(ctx context.Context, _ map[string]any, _ Handle) (ApplyResult, error) {
	return ApplyResult{}, m.block(ctx)
}

// TestCheck_CancelReachesPluginContext asserts that cancelling the host's ctx
// cancels the ctx the plugin's Check received. Without the cancel notification
// the plugin would block until the process was killed and the parameter would
// be decorative.
func TestCheck_CancelReachesPluginContext(t *testing.T) {
	mod := newCancelAwareModule()
	c := newClient(t, mod, TargetInfo{}, NoopHandleServer())
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Check(ctx, map[string]any{}, nil)
		errCh <- err
	}()

	<-mod.entered
	cancel()

	select {
	case observed := <-mod.observed:
		if !errors.Is(observed, context.Canceled) {
			t.Fatalf("plugin observed %v, want context.Canceled", observed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("plugin context was never cancelled")
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Check returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Check never returned after cancellation")
	}
}

// TestApply_CancelReachesPluginContext is the Apply-side counterpart.
func TestApply_CancelReachesPluginContext(t *testing.T) {
	mod := newCancelAwareModule()
	c := newClient(t, mod, TargetInfo{}, NoopHandleServer())
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Apply(ctx, map[string]any{}, nil)
		errCh <- err
	}()

	<-mod.entered
	cancel()

	select {
	case observed := <-mod.observed:
		if !errors.Is(observed, context.Canceled) {
			t.Fatalf("plugin observed %v, want context.Canceled", observed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("plugin context was never cancelled")
	}

	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply returned %v, want context.Canceled", err)
	}
}

// runCommandModule forwards the Check context straight into a handle op, which
// is what plugin authors are told to do.
type runCommandModule struct{}

func (runCommandModule) Name() string    { return "runcommand-mock" }
func (runCommandModule) Version() string { return "1.0.0" }

func (runCommandModule) Check(ctx context.Context, _ map[string]any, h Handle) (CheckResult, error) {
	if _, err := h.RunCommand(ctx, "sleep forever"); err != nil {
		return CheckResult{}, err
	}
	return CheckResult{}, nil
}

func (runCommandModule) Apply(context.Context, map[string]any, Handle) (ApplyResult, error) {
	return ApplyResult{}, nil
}

// blockingHandleServer stands in for a slow target op (a long RunCommand over
// WinRM, say). It blocks until its own context is cancelled and reports what
// it saw.
type blockingHandleServer struct {
	started  chan struct{}
	observed chan error
}

func newBlockingHandleServer() *blockingHandleServer {
	return &blockingHandleServer{
		started:  make(chan struct{}),
		observed: make(chan error, 1),
	}
}

func (s *blockingHandleServer) RunCommand(ctx context.Context, _ string) (CommandResult, error) {
	close(s.started)
	<-ctx.Done()
	s.observed <- ctx.Err()
	return CommandResult{}, ctx.Err()
}

func (s *blockingHandleServer) PutFile(context.Context, string, []byte) error { return nil }

func (s *blockingHandleServer) GetFile(context.Context, string) ([]byte, error) { return nil, nil }

// TestCheck_CancelInterruptsInFlightHandleOp asserts the full round trip:
// host cancels check → plugin's Check context fires → the plugin's in-flight
// RunCommand is abandoned → the host cancels the target op it was serving.
// This is the property that makes cancellation useful rather than cosmetic,
// because the expensive work lives in the target op, not in the plugin.
func TestCheck_CancelInterruptsInFlightHandleOp(t *testing.T) {
	ops := newBlockingHandleServer()
	c := newClient(t, runCommandModule{}, TargetInfo{}, ops)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Check(ctx, map[string]any{}, nil)
		errCh <- err
	}()

	<-ops.started
	cancel()

	select {
	case observed := <-ops.observed:
		if !errors.Is(observed, context.Canceled) {
			t.Fatalf("target op observed %v, want context.Canceled", observed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("target op context was never cancelled")
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Check returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Check never returned after cancellation")
	}
}

// stubbornModule ignores its context entirely, standing in for a plugin whose
// author did not honour cancellation.
type stubbornModule struct{}

func (stubbornModule) Name() string    { return "stubborn-mock" }
func (stubbornModule) Version() string { return "1.0.0" }

func (stubbornModule) Check(context.Context, map[string]any, Handle) (CheckResult, error) {
	time.Sleep(time.Second)
	return CheckResult{}, nil
}

func (stubbornModule) Apply(context.Context, map[string]any, Handle) (ApplyResult, error) {
	return ApplyResult{}, nil
}

// TestCheck_CancelGraceIsBounded asserts the grace window is an upper bound,
// not a promise: a plugin that ignores its context does not hold the host
// hostage past cancelGrace.
func TestCheck_CancelGraceIsBounded(t *testing.T) {
	c := newClient(t, stubbornModule{}, TargetInfo{}, NoopHandleServer())
	defer func() { _ = c.Close() }()

	// Shrink the grace window before any call is in flight, so the only
	// reader (abandon, on this goroutine) is ordered after the write.
	c.codec.cancelGrace = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := c.Check(ctx, map[string]any{}, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Check returned %v, want context.Canceled", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Check took %v; grace window did not bound the wait", elapsed)
	}
}
