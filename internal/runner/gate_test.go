package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
	"github.com/bluecadet/preflight/internal/template"
)

// posixFake builds a targettest.Fake whose Info() resolves a posix-shell
// runtime over SSH — the case the apply-start gate exists for (plan-time can
// only name-check SSH; the runtime is only known after Info()).
func posixFake(results ...target.Result) *targettest.Fake {
	return &targettest.Fake{
		InfoValue: target.TargetInfo{
			Hostname:    "posix-host",
			OSFamily:    target.OSFamilyLinux,
			Transport:   target.TransportSSH,
			RuntimeKind: target.RuntimeKindPOSIXShell,
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

func TestApplyGate_RefusesUnsupportedPreTask1(t *testing.T) {
	fake := posixFake()
	plan := gatePlan(
		gateTask("t-shell", "ok task", "shell"),
		gateTask("t-reg", "windows task", "registry"),
	)
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec})

	err := r.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected gate refusal error, got nil")
	}

	g := findGateEvent(rec)
	if g == nil {
		t.Fatalf("expected support_gate event, got: %v", rec.events)
	}
	if g.Runtime != "posix-shell" {
		t.Errorf("runtime = %q, want posix-shell", g.Runtime)
	}
	if g.Reason != "unsupported_on_runtime" {
		t.Errorf("reason = %q, want unsupported_on_runtime", g.Reason)
	}
	if len(g.Violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(g.Violations))
	}
	v := g.Violations[0]
	if v.Module != "registry" || v.TaskName != "windows task" {
		t.Errorf("violation = {%s, %s}, want {registry, windows task}", v.Module, v.TaskName)
	}
	if v.Reason != "unsupported_on_runtime" {
		t.Errorf("violation reason = %q, want unsupported_on_runtime", v.Reason)
	}
	if !strings.Contains(g.LogMessage(), "1 task(s)") || !strings.Contains(g.LogMessage(), "posix-shell") {
		t.Errorf("summary msg = %q, want count + runtime", g.LogMessage())
	}

	// Refused before task 1: nothing executed.
	if len(fake.Calls) != 0 {
		t.Fatalf("expected 0 Execute calls (refused pre-task-1), got %d", len(fake.Calls))
	}
}

func TestApplyGate_ListsAllViolations(t *testing.T) {
	fake := posixFake()
	// registry, service, package are all Windows-only on posix-shell.
	plan := gatePlan(
		gateTask("t-reg", "reg", "registry"),
		gateTask("t-svc", "svc", "service"),
		gateTask("t-pkg", "pkg", "package"),
	)
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec})

	err := r.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected gate refusal")
	}
	g := findGateEvent(rec)
	if g == nil {
		t.Fatal("expected support_gate event")
	}
	if len(g.Violations) != 3 {
		t.Fatalf("violations = %d, want 3", len(g.Violations))
	}
	wantModules := map[string]bool{"registry": false, "service": false, "package": false}
	for _, v := range g.Violations {
		wantModules[v.Module] = true
	}
	for mod, seen := range wantModules {
		if !seen {
			t.Errorf("missing violation for module %q", mod)
		}
	}
	if !strings.Contains(g.LogMessage(), "3 task(s)") {
		t.Errorf("summary = %q, want count 3", g.LogMessage())
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("expected 0 Execute calls, got %d", len(fake.Calls))
	}
}

func TestApplyGate_WhenFalseTaskExcluded(t *testing.T) {
	fake := posixFake(target.Result{Status: target.StatusOK})
	// registry is unsupported on posix but guarded by a when-condition that is
	// false — a cross-platform playbook branch. The gate excludes it; the
	// supported shell task runs.
	regTask := gateTask("t-reg", "windows branch", "registry")
	regTask.When = "{{ vars.run_windows }}"
	regTask.Scope = template.NewScope(map[string]any{"run_windows": false})
	shellTask := gateTask("t-shell", "posix branch", "shell")

	plan := gatePlan(regTask, shellTask)
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec})

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("gate should not refuse a when-guarded playbook: %v", err)
	}
	if g := findGateEvent(rec); g != nil {
		t.Fatalf("did not expect support_gate event: %+v", g)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("expected 1 Execute call (shell only), got %d", len(fake.Calls))
	}
	if fake.Calls[0].Module != "shell" {
		t.Errorf("executed module = %q, want shell", fake.Calls[0].Module)
	}
}

func TestApplyGate_IgnoreErrorsExempt(t *testing.T) {
	// An unsupported module on an ignore_errors task is exempt from the gate:
	// it keeps fail-and-continue at execution time. The gate does not refuse;
	// both tasks reach Execute.
	fake := posixFake(
		target.Result{Status: target.StatusFailed, Message: "unsupported at runtime"},
		target.Result{Status: target.StatusOK},
	)
	regTask := gateTask("t-reg", "best-effort windows task", "registry")
	regTask.IgnoreErrors = true
	shellTask := gateTask("t-shell", "posix task", "shell")

	plan := gatePlan(regTask, shellTask)
	rec := &recordingRenderer{}
	r := New(fake, emptyResolver(), Config{Renderer: rec})

	if err := r.Apply(context.Background(), plan); err == nil {
		// finalizeApply returns an error when failedCount > 0; the ignored
		// failure counts. The point is that the *gate* did not refuse —
		// execution proceeded.
		_ = err
	}
	if g := findGateEvent(rec); g != nil {
		t.Fatalf("ignore_errors task should be gate-exempt, got support_gate: %+v", g)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("expected 2 Execute calls (both tasks ran), got %d", len(fake.Calls))
	}
}

func TestApplyGate_PluginBypasses(t *testing.T) {
	pluginReg := target.ModuleRegistry{
		"custom": pluginStub{},
	}
	fake := posixFake(target.Result{Status: target.StatusOK}, target.Result{Status: target.StatusOK})
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
	fake := posixFake(target.Result{Status: target.StatusOK}, target.Result{Status: target.StatusOK})
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

// pluginStub satisfies target.Module + PluggableModule so it is recognized as a
// plugin by IsPluginModule (bypasses the runtime matrix).
type pluginStub struct{}

func (pluginStub) Check(context.Context, map[string]any, target.OutputFunc) (target.CheckResult, error) {
	return target.CheckResult{}, nil
}
func (pluginStub) Apply(context.Context, map[string]any, target.OutputFunc) (target.ApplyResult, error) {
	return target.ApplyResult{}, nil
}
func (pluginStub) PluginPath() string         { return "/tmp/custom" }
func (pluginStub) CloneModule() target.Module { return pluginStub{} }

// Ensure the refusal error is typed so callers can distinguish a gate refusal
// from a task-failure summary.
func TestApplyGate_RefusalIsTyped(t *testing.T) {
	fake := posixFake()
	plan := gatePlan(gateTask("t-reg", "windows task", "registry"))
	r := New(fake, emptyResolver(), Config{Renderer: &recordingRenderer{}})

	err := r.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected refusal error")
	}
	var refusal *GateRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("expected *GateRefusal, got %T: %v", err, err)
	}
	if refusal.RuntimeKind != target.RuntimeKindPOSIXShell {
		t.Errorf("refusal runtime = %q, want posix-shell", refusal.RuntimeKind)
	}
	if len(refusal.Violations) != 1 {
		t.Errorf("refusal violations = %d, want 1", len(refusal.Violations))
	}
}
