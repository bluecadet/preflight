package target

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type mockAdapterModule struct {
	checkResult CheckResult
	checkErr    error
	applyResult ApplyResult
	applyErr    error
	outputLines []string
}

func (m *mockAdapterModule) Check(_ context.Context, _ map[string]any, out OutputFunc) (CheckResult, error) {
	for _, line := range m.outputLines {
		if out != nil {
			out(line)
		}
	}
	return m.checkResult, m.checkErr
}

func (m *mockAdapterModule) Apply(_ context.Context, _ map[string]any, out OutputFunc) (ApplyResult, error) {
	for _, line := range m.outputLines {
		if out != nil {
			out(line)
		}
	}
	return m.applyResult, m.applyErr
}

// noopHandle is a Handle whose target ops are never exercised by built-in
// modules; it only carries Output for streaming.
type noopHandle struct{}

func (noopHandle) RunCommand(context.Context, string) (sdk.CommandResult, error) {
	return sdk.CommandResult{}, nil
}
func (noopHandle) PutFile(context.Context, string, []byte) error   { return nil }
func (noopHandle) GetFile(context.Context, string) ([]byte, error) { return nil, nil }
func (noopHandle) Info() sdk.TargetInfo                            { return sdk.TargetInfo{} }
func (noopHandle) Output(string)                                   {}

func TestSDKModuleAdapter_Check(t *testing.T) {
	mod := &mockAdapterModule{
		checkResult: CheckResult{NeedsChange: true, Message: "needs update"},
	}
	adapter := NewSDKModuleAdapter("test-module", mod)

	result, err := adapter.Check(map[string]any{"key": "value"}, noopHandle{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NeedsChange {
		t.Errorf("expected NeedsChange=true")
	}
	if result.Message != "needs update" {
		t.Errorf("expected message 'needs update', got %q", result.Message)
	}
}

func TestSDKModuleAdapter_CheckStreaming(t *testing.T) {
	mod := &mockAdapterModule{
		checkResult: CheckResult{NeedsChange: false},
		outputLines: []string{"line 1", "line 2"},
	}
	adapter := NewSDKModuleAdapter("test-module", mod)

	var received []string
	h := recordingHandle{out: func(line string) { received = append(received, line) }}
	result, err := adapter.Check(map[string]any{}, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NeedsChange {
		t.Errorf("expected NeedsChange=false")
	}
	if len(received) != 2 {
		t.Errorf("expected 2 output lines, got %d: %v", len(received), received)
	}
}

func TestSDKModuleAdapter_Apply(t *testing.T) {
	mod := &mockAdapterModule{
		applyResult: ApplyResult{Message: "applied"},
	}
	adapter := NewSDKModuleAdapter("test-module", mod)

	result, err := adapter.Apply(map[string]any{}, noopHandle{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Message != "applied" {
		t.Errorf("expected message 'applied', got %q", result.Message)
	}
}

func TestSDKModuleAdapter_NilHandleOutput(t *testing.T) {
	mod := &mockAdapterModule{
		checkResult: CheckResult{NeedsChange: true},
		outputLines: []string{"some output"},
	}
	adapter := NewSDKModuleAdapter("test-module", mod)

	// A nil handle (host passed nil) should not panic.
	_, err := adapter.Check(map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type recordingHandle struct {
	out func(line string)
}

func (h recordingHandle) RunCommand(context.Context, string) (sdk.CommandResult, error) {
	return sdk.CommandResult{}, nil
}
func (h recordingHandle) PutFile(context.Context, string, []byte) error   { return nil }
func (h recordingHandle) GetFile(context.Context, string) ([]byte, error) { return nil, nil }
func (h recordingHandle) Info() sdk.TargetInfo                            { return sdk.TargetInfo{} }
func (h recordingHandle) Output(line string) {
	if h.out != nil {
		h.out(line)
	}
}
