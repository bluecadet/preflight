package runner

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
	"github.com/bluecadet/preflight/internal/template"
)

// windowsFake builds a targettest.Fake whose Info() resolves a
// windows-powershell runtime over SSH.
func windowsFake(results ...target.Result) *targettest.Fake {
	return &targettest.Fake{
		InfoValue: target.TargetInfo{
			Hostname:    "win-host",
			OSFamily:    target.OSFamilyWindows,
			Transport:   target.TransportSSH,
			RuntimeKind: target.RuntimeKindWindowsPowerShell,
		},
		Results: results,
	}
}

func gatePlan(tasks ...*PlanTask) *ExecutionPlan {
	return &ExecutionPlan{PlaybookName: "gate-test", Tasks: tasks}
}

func gateTask(id, name, module string) *PlanTask {
	return &PlanTask{
		ID:     id,
		Name:   name,
		Module: module,
		Params: map[string]any{},
		Scope:  template.NewScope(),
	}
}

// findGateEvent returns the first SupportGateEvent emitted to rec, or nil.
func findGateEvent(rec *recordingRenderer) *output.SupportGateEvent {
	for _, e := range rec.events {
		if g, ok := e.(output.SupportGateEvent); ok {
			return &g
		}
	}
	return nil
}

func TestApplyGate_PluginBypasses(t *testing.T) {
	pluginReg := target.ModuleRegistry{
		"custom": pluginStub{},
	}
	fake := windowsFake(target.Result{Status: target.StatusOK}, target.Result{Status: target.StatusOK})
	plan := gatePlan(
		gateTask("t-plugin", "plugin task", "custom"),
		gateTask("t-shell", "shell task", "shell"),
	)
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec, ModuleRegistry: pluginReg})

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("plugin should bypass gate: %v", err)
	}
	if g := findGateEvent(rec); g != nil {
		t.Fatalf("did not expect support_gate for plugin: %+v", g)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("expected 2 Execute calls, got %d", len(fake.Calls))
	}
}

func TestApplyGate_SupportedOnlyPasses(t *testing.T) {
	fake := windowsFake(target.Result{Status: target.StatusOK}, target.Result{Status: target.StatusOK})
	plan := gatePlan(
		gateTask("t-shell", "shell", "shell"),
		gateTask("t-file", "file", "file"),
	)
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec})

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("supported-only plan should pass gate: %v", err)
	}
	if g := findGateEvent(rec); g != nil {
		t.Fatalf("did not expect support_gate: %+v", g)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("expected 2 Execute calls, got %d", len(fake.Calls))
	}
}

// TestApplyGate_RuntimeUnresolvedSkipsGate guards the no-op path: if a target
// does not populate RuntimeKind (e.g. a future transport), the gate is skipped
// rather than refusing or panicking. This keeps the gate target-agnostic.
// The skip now logs a warning (see TestApplyGate_RuntimeUnresolvedLogsWarning)
// so a misconfigured transport is observable instead of silently disabling
// support validation.
func TestApplyGate_RuntimeUnresolvedSkipsGate(t *testing.T) {
	fake := &targettest.Fake{
		InfoValue: target.TargetInfo{Transport: target.TransportSSH}, // RuntimeKind empty
		Results:   []target.Result{{Status: target.StatusOK}},
	}
	plan := gatePlan(gateTask("t-reg", "windows task", "registry"))
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec})

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("gate should be skipped when runtime is unresolved: %v", err)
	}
	if g := findGateEvent(rec); g != nil {
		t.Fatalf("did not expect support_gate when runtime unresolved: %+v", g)
	}
}

// TestApplyGate_RuntimeUnresolvedLogsWarning verifies that when RuntimeKind is
// unresolved the gate skip is logged at warn level with the target name, so a
// transport that forgets to populate RuntimeKind is not silently disabling the
// support gate.
func TestApplyGate_RuntimeUnresolvedLogsWarning(t *testing.T) {
	var buf strings.Builder
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	fake := &targettest.Fake{
		InfoValue: target.TargetInfo{Transport: target.TransportSSH, Hostname: "unresolved-host"},
		Results:   []target.Result{{Status: target.StatusOK}},
	}
	plan := gatePlan(gateTask("t-reg", "windows task", "registry"))
	r := New(fake, emptyResolver(), Config{Renderer: &recordingRenderer{}})

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("gate should be skipped when runtime is unresolved: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "level=WARN") {
		t.Errorf("expected a WARN-level log, got: %s", logged)
	}
	if !strings.Contains(logged, "support gate skipped") {
		t.Errorf("expected support gate skipped warning, got: %s", logged)
	}
}

// pluginStub satisfies target.Module + PluggableModule so it is recognized as a
// plugin by IsPluginModule (bypasses the runtime matrix).
type pluginStub struct{}

func (pluginStub) Check(context.Context, map[string]any, target.OutputFunc) (target.CheckResult, error) {
	return target.CheckResult{}, nil
}
func (pluginStub) Apply(context.Context, map[string]any, target.OutputFunc) (target.ApplyResult, error) {
	return target.ApplyResult{}, nil
}
func (pluginStub) PluginPath() string                            { return "/tmp/custom" }
func (pluginStub) BindTarget(ops target.TargetOps) target.Module { return pluginStub{} }
