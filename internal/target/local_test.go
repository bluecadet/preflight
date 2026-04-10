package target_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/bluecadet/preflight/internal/target"
)

// mockModule is a configurable Module for testing.
type mockModule struct {
	needsChange bool
	checkErr    error
	applyErr    error
	applyCalled bool
}

func (m *mockModule) Check(_ context.Context, _ map[string]any) (bool, error) {
	return m.needsChange, m.checkErr
}

func (m *mockModule) Apply(_ context.Context, _ map[string]any) error {
	m.applyCalled = true
	return m.applyErr
}

func TestLocalTarget_UnknownModule(t *testing.T) {
	tgt := target.NewLocalTarget(nil)
	_, err := tgt.Execute(context.Background(), "task-1", "nonexistent", nil, target.ExecutionOptions{}, false, nil)
	if err == nil {
		t.Fatal("expected error for unknown module, got nil")
	}
}

func TestLocalTarget_AlreadyInDesiredState(t *testing.T) {
	mod := &mockModule{needsChange: false}
	registry := target.ModuleRegistry{"noop": mod}
	tgt := target.NewLocalTarget(registry)

	result, err := tgt.Execute(context.Background(), "task-2", "noop", nil, target.ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != target.StatusOK {
		t.Errorf("expected StatusOK, got %q", result.Status)
	}
	if mod.applyCalled {
		t.Error("Apply should not have been called when no change is needed")
	}
}

func TestLocalTarget_DryRunWithPendingChange(t *testing.T) {
	mod := &mockModule{needsChange: true}
	registry := target.ModuleRegistry{"pending": mod}
	tgt := target.NewLocalTarget(registry)

	result, err := tgt.Execute(context.Background(), "task-3", "pending", nil, target.ExecutionOptions{}, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != target.StatusChanged {
		t.Errorf("expected StatusChanged, got %q", result.Status)
	}
	if mod.applyCalled {
		t.Error("Apply must not be called during a dry-run")
	}
}

func TestLocalTarget_ApplyCalledWhenChangeNeeded(t *testing.T) {
	mod := &mockModule{needsChange: true}
	registry := target.ModuleRegistry{"changer": mod}
	tgt := target.NewLocalTarget(registry)

	result, err := tgt.Execute(context.Background(), "task-4", "changer", nil, target.ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != target.StatusChanged {
		t.Errorf("expected StatusChanged, got %q", result.Status)
	}
	if !mod.applyCalled {
		t.Error("Apply should have been called when a change is needed")
	}
}

func TestLocalTarget_CheckError(t *testing.T) {
	mod := &mockModule{checkErr: errors.New("check failed")}
	registry := target.ModuleRegistry{"errmod": mod}
	tgt := target.NewLocalTarget(registry)

	result, err := tgt.Execute(context.Background(), "task-5", "errmod", nil, target.ExecutionOptions{}, false, nil)
	if err == nil {
		t.Fatal("expected error from Check, got nil")
	}
	if result.Status != target.StatusFailed {
		t.Errorf("expected StatusFailed, got %q", result.Status)
	}
}

// streamingMockModule is a Module that also implements StreamingModule.
// It emits a fixed set of lines via the onOutput callback.
type streamingMockModule struct {
	lines []string
}

func (m *streamingMockModule) Check(_ context.Context, _ map[string]any) (bool, error) {
	return true, nil // always needs change
}

func (m *streamingMockModule) Apply(_ context.Context, _ map[string]any) error {
	return nil
}

func (m *streamingMockModule) ApplyWithOutput(_ context.Context, _ map[string]any, onOutput target.OutputFunc) error {
	for _, line := range m.lines {
		if onOutput != nil {
			onOutput(line)
		}
	}
	return nil
}

type checkStreamingMockModule struct {
	lines []string
}

func (m *checkStreamingMockModule) Check(_ context.Context, _ map[string]any) (bool, error) {
	return true, nil
}

func (m *checkStreamingMockModule) CheckWithOutput(_ context.Context, _ map[string]any, onOutput target.OutputFunc) (bool, error) {
	for _, line := range m.lines {
		if onOutput != nil {
			onOutput(line)
		}
	}
	return true, nil
}

func (m *checkStreamingMockModule) Apply(_ context.Context, _ map[string]any) error {
	return nil
}

func TestLocalTarget_Execute_StreamingOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping streaming output test on Windows")
	}

	wantLines := []string{"hello", "world"}
	mod := &streamingMockModule{lines: wantLines}
	registry := target.ModuleRegistry{"streamer": mod}
	tgt := target.NewLocalTarget(registry)

	var received []string
	result, err := tgt.Execute(context.Background(), "task-stream", "streamer", nil, target.ExecutionOptions{}, false, func(line string) {
		received = append(received, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != target.StatusChanged {
		t.Errorf("expected StatusChanged, got %q", result.Status)
	}

	// Both result.Output and the onOutput callback should have the same lines.
	if len(result.Output) != len(wantLines) {
		t.Fatalf("result.Output: expected %d lines, got %d: %v", len(wantLines), len(result.Output), result.Output)
	}
	if len(received) != len(wantLines) {
		t.Fatalf("onOutput callback: expected %d lines, got %d: %v", len(wantLines), len(received), received)
	}
	for i, want := range wantLines {
		if result.Output[i] != want {
			t.Errorf("result.Output[%d]: expected %q, got %q", i, want, result.Output[i])
		}
		if received[i] != want {
			t.Errorf("received[%d]: expected %q, got %q", i, want, received[i])
		}
	}
}

func TestLocalTarget_Execute_CheckStreamingOutputDuringDryRun(t *testing.T) {
	wantLines := []string{"checking appx package A", "checking appx package B"}
	mod := &checkStreamingMockModule{lines: wantLines}
	registry := target.ModuleRegistry{"streamer": mod}
	tgt := target.NewLocalTarget(registry)

	var received []string
	result, err := tgt.Execute(context.Background(), "task-stream", "streamer", nil, target.ExecutionOptions{}, true, func(line string) {
		received = append(received, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != target.StatusChanged {
		t.Errorf("expected StatusChanged, got %q", result.Status)
	}
	if len(result.Output) != len(wantLines) {
		t.Fatalf("result.Output: expected %d lines, got %d: %v", len(wantLines), len(result.Output), result.Output)
	}
	if len(received) != len(wantLines) {
		t.Fatalf("onOutput callback: expected %d lines, got %d: %v", len(wantLines), len(received), received)
	}
	for i, want := range wantLines {
		if result.Output[i] != want {
			t.Errorf("result.Output[%d]: expected %q, got %q", i, want, result.Output[i])
		}
		if received[i] != want {
			t.Errorf("received[%d]: expected %q, got %q", i, want, received[i])
		}
	}
}
