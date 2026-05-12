package target

import (
	"context"
	"testing"
)

// closeableModule is a minimal Module that records Close() invocations so
// the test can verify that LocalTarget.Close propagates the call to each
// registered module that owns resources.
type closeableModule struct {
	needsChange bool
	closeCalls  int
}

func (m *closeableModule) Check(context.Context, map[string]any) (bool, error) {
	return m.needsChange, nil
}

func (m *closeableModule) Apply(context.Context, map[string]any) error { return nil }

func (m *closeableModule) Close() error { m.closeCalls++; return nil }

func TestLocalTargetCloseClosesModuleResources(t *testing.T) {
	mod := &closeableModule{needsChange: true}
	tgt := NewLocalTarget(ModuleRegistry{"custom": mod})

	if _, err := tgt.Execute(context.Background(), "task-1", "custom", nil, ExecutionOptions{}, false, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if mod.closeCalls != 0 {
		t.Fatalf("Close called %d times before target shutdown, want 0", mod.closeCalls)
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("target Close() error = %v", err)
	}
	if mod.closeCalls != 1 {
		t.Fatalf("Close called %d times after target shutdown, want 1", mod.closeCalls)
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("second target Close() error = %v", err)
	}
	if mod.closeCalls != 1 {
		t.Fatalf("Close called %d times after second target shutdown, want 1", mod.closeCalls)
	}
}
