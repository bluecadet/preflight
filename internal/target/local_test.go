package target_test

import (
	"context"
	"errors"
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

func (m *mockModule) Check(_ context.Context, _ map[string]interface{}) (bool, error) {
	return m.needsChange, m.checkErr
}

func (m *mockModule) Apply(_ context.Context, _ map[string]interface{}) error {
	m.applyCalled = true
	return m.applyErr
}

func TestLocalTarget_UnknownModule(t *testing.T) {
	tgt := target.NewLocalTarget(nil)
	_, err := tgt.Execute(context.Background(), "task-1", "nonexistent", nil, false)
	if err == nil {
		t.Fatal("expected error for unknown module, got nil")
	}
}

func TestLocalTarget_AlreadyInDesiredState(t *testing.T) {
	mod := &mockModule{needsChange: false}
	registry := target.ModuleRegistry{"noop": mod}
	tgt := target.NewLocalTarget(registry)

	result, err := tgt.Execute(context.Background(), "task-2", "noop", nil, false)
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

	result, err := tgt.Execute(context.Background(), "task-3", "pending", nil, true)
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

	result, err := tgt.Execute(context.Background(), "task-4", "changer", nil, false)
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

	result, err := tgt.Execute(context.Background(), "task-5", "errmod", nil, false)
	if err == nil {
		t.Fatal("expected error from Check, got nil")
	}
	if result.Status != target.StatusFailed {
		t.Errorf("expected StatusFailed, got %q", result.Status)
	}
}
