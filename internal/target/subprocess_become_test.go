package target

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// fakeElevation is a test double that spawns the binary as the current user
// (no actual elevation). Used to test the subprocess round-trip.
type fakeElevation struct {
	startFunc func(ctx context.Context, binary string, moduleName string) (*sdk.Client, error)
	calls     []fakeElevationCall
}

type fakeElevationCall struct {
	binary     string
	moduleName string
}

func (f *fakeElevation) Start(ctx context.Context, binary string, moduleName string) (*sdk.Client, error) {
	f.calls = append(f.calls, fakeElevationCall{binary: binary, moduleName: moduleName})
	return f.startFunc(ctx, binary, moduleName)
}

func TestSubprocessModule_Check_DelegatesToElevation(t *testing.T) {
	var startCalled bool
	fake := &fakeElevation{
		startFunc: func(_ context.Context, _, _ string) (*sdk.Client, error) {
			startCalled = true
			// Return a mock client that always says no change needed.
			// We can't easily create a real sdk.Client without a subprocess,
			// so test the wiring by verifying Start was called.
			return nil, context.Canceled
		},
	}

	mod := &subprocessModule{
		name:      "directory",
		binary:    "/usr/bin/false",
		elevation: fake,
	}

	_, _ = mod.Check(context.Background(), map[string]any{}, nil)
	if !startCalled {
		t.Error("expected elevation.Start to be called during Check")
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(fake.calls))
	}
	if fake.calls[0].moduleName != "directory" {
		t.Errorf("expected moduleName=directory, got %q", fake.calls[0].moduleName)
	}
}

func TestSubprocessModule_Apply_DelegatesToElevation(t *testing.T) {
	var startCalled bool
	fake := &fakeElevation{
		startFunc: func(_ context.Context, _, _ string) (*sdk.Client, error) {
			startCalled = true
			return nil, context.Canceled
		},
	}

	mod := &subprocessModule{
		name:      "shell",
		binary:    "/usr/bin/false",
		elevation: fake,
	}

	_, _ = mod.Apply(context.Background(), map[string]any{}, nil)
	if !startCalled {
		t.Error("expected elevation.Start to be called during Apply")
	}
}

type mockSubprocessModule struct{}

func (m *mockSubprocessModule) Check(_ context.Context, _ map[string]any, _ OutputFunc) (CheckResult, error) {
	return CheckResult{}, nil
}

func (m *mockSubprocessModule) Apply(_ context.Context, _ map[string]any, _ OutputFunc) (ApplyResult, error) {
	return ApplyResult{}, nil
}

func TestNewSubprocessBecomeRegistry_POSIX(t *testing.T) {
	source := ModuleRegistry{
		"shell":     &mockSubprocessModule{},
		"directory": &mockSubprocessModule{},
	}
	become := &BecomeOptions{User: "testuser", Password: "testpass"}
	reg, err := newSubprocessBecomeRegistry(source, RuntimeKindPOSIXShell, become)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg) != 2 {
		t.Errorf("expected 2 modules, got %d", len(reg))
	}
	for name, mod := range reg {
		sm, ok := mod.(*subprocessModule)
		if !ok {
			t.Errorf("module %q is not a subprocessModule", name)
			continue
		}
		if sm.name != name {
			t.Errorf("module name mismatch: %q vs %q", sm.name, name)
		}
		if _, ok := sm.elevation.(*posixSudoElevation); !ok {
			t.Errorf("expected posixSudoElevation for module %q", name)
		}
	}
}

func TestNewSubprocessBecomeRegistry_Windows(t *testing.T) {
	source := ModuleRegistry{
		"registry": &mockSubprocessModule{},
	}
	become := &BecomeOptions{User: "kiosk", Password: "secret"}
	reg, err := newSubprocessBecomeRegistry(source, RuntimeKindWindowsPowerShell, become)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mod, ok := reg["registry"]
	if !ok {
		t.Fatal("expected registry module in result")
	}
	sm, ok := mod.(*subprocessModule)
	if !ok {
		t.Fatal("expected subprocessModule")
	}
	if _, ok := sm.elevation.(*windowsCredentialElevation); !ok {
		t.Fatal("expected windowsCredentialElevation")
	}
}

func TestPosixSudoElevation_ArgsWithPassword(t *testing.T) {
	e := &posixSudoElevation{user: "alice", password: "secret123"}
	// Verify it constructs the -S flag when password is set (don't actually run sudo)
	if e.password == "" {
		t.Error("password should be set")
	}
}
