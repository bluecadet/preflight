package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/preflighterr"
	"github.com/bluecadet/preflight/internal/schemavalidation"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
	"github.com/bluecadet/preflight/internal/template"
)

// validateRunLogFile reads path (a RunLogSink-written JSONL file) and
// validates every line against the real embedded runlog.schema.json via
// schemavalidation.ValidateDocument — the same compiled-schema cache every
// other document type in this package uses. This is how schema drift
// between the Go event structs and the schema gets caught: by feeding the
// schema real output from the real emission code path (apply.go), not a
// hand-written JSON fixture standing in for one.
func validateRunLogFile(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("run log is empty")
	}
	for i, line := range lines {
		var doc any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d: invalid JSON: %v\n%s", i+1, err, line)
		}
		if err := schemavalidation.ValidateDocument(doc, schemavalidation.RunLogSchemaURL); err != nil {
			t.Fatalf("line %d: schema validation failed: %v\n%s", i+1, err, line)
		}
	}
}

// TestRunLogSchema_RealApplyOutputValidatesAgainstSchema drives the real
// apply.go task loop (tag filtering, when-condition evaluation, module
// execution, and the module-reported-skip branch) through a real
// output.RunLogSink, then validates every emitted line against
// runlog.schema.json. It is intentionally not a hand-written JSONL
// fixture: every line comes from applyResolved actually running.
func TestRunLogSchema_RealApplyOutputValidatesAgainstSchema(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	sink, err := output.NewRunLogSink("schema-run", logPath)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	allTags := []string{"all"}

	tasks := []*PlanTask{
		{
			ID:     "task-tagfiltered",
			Name:   "filtered out",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(),
			Tags:   []string{"never"},
		},
		{
			ID:     "task-whenfalse",
			Name:   "skipped by when",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(map[string]any{"should_run": false}),
			Tags:   allTags,
			When:   "{{ vars.should_run }}",
		},
		{
			ID:     "task-ok",
			Name:   "already correct",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(),
			Tags:   allTags,
		},
		{
			ID:     "task-changed",
			Name:   "applies a change",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(),
			Tags:   allTags,
		},
		{
			ID:     "task-already-satisfied",
			Name:   "module reports its own skip",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(),
			Tags:   allTags,
		},
		{
			ID:           "task-failed-ignored",
			Name:         "fails but ignored",
			Module:       "shell",
			Params:       map[string]any{"cmd": "echo"},
			Scope:        template.NewScope(),
			Tags:         allTags,
			IgnoreErrors: true,
		},
	}

	dag, err := BuildDAG(tasks)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}

	fake := &targettest.Fake{
		InfoValue: target.TargetInfo{Transport: target.TransportLocal},
		// Indexed by Execute call order, which — for this fully independent
		// task set — matches the task list order above, skipping the two
		// tasks that never call Execute (tag-filtered, when-false).
		Results: []target.Result{
			{Status: target.StatusOK},
			{Status: target.StatusChanged},
			{Status: target.StatusSkipped, Message: "already installed at the requested version"},
			{Status: target.StatusFailed, Message: "boom"},
		},
		// Exercises task_output too: every Execute call replays this
		// through onOutput, emitting a real TaskOutputEvent per task.
		Output: []string{"module output line"},
	}

	r := New(fake, emptyResolver(), Config{Tags: allTags, Renderer: sink})
	rt := &template.RuntimeContext{Target: map[string]any{}, Facts: map[string]any{}, Env: map[string]string{}}

	// The ignored task still fails (ignore_errors only exempts it from
	// halting the loop), so applyResolved still reports the run as failed
	// overall — see TestApplyIgnoreErrorsContinuesToLaterTasks.
	applyErr := r.applyResolved(context.Background(), dag, rt)
	if applyErr == nil {
		t.Fatal("expected applyResolved to report the ignored failure")
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("RunLogSink.Close: %v", err)
	}

	validateRunLogFile(t, logPath)
}

// TestRunLogSchema_DependencyFailedSkipValidatesAgainstSchema exercises the
// "dependency-failed" task_skipped branch directly. In the current
// sequential apply loop a non-ignored failure halts before any dependent
// task is ever considered (see TestApplyResolvedSkippedByDependencyFailure),
// so this reaches the branch the way a future concurrent/partial-failure
// scheduler would: by calling the same real executeTask used by
// applyResolved with a pre-populated failed-dependency set, rather than by
// hand-writing the JSON the branch would produce.
func TestRunLogSchema_DependencyFailedSkipValidatesAgainstSchema(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	sink, err := output.NewRunLogSink("schema-run-dep", logPath)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	dependent := &PlanTask{
		ID:        "task-dependent",
		Name:      "depends on a failed task",
		Module:    "shell",
		Params:    map[string]any{"cmd": "echo"},
		Scope:     template.NewScope(),
		Tags:      []string{"all"},
		DependsOn: []string{"fatal"},
	}
	tasks := []*PlanTask{
		{ID: "fatal", Name: "fatal", Module: "shell", Params: map[string]any{"cmd": "echo"}, Scope: template.NewScope(), Tags: []string{"all"}},
		dependent,
	}
	dag, err := BuildDAG(tasks)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}

	fake := &targettest.Fake{InfoValue: target.TargetInfo{Transport: target.TransportLocal}}
	r := New(fake, emptyResolver(), Config{Tags: []string{"all"}, Renderer: sink})
	rt := &template.RuntimeContext{Target: map[string]any{}, Facts: map[string]any{}, Env: map[string]string{}}

	acc := newApplyAccumulator()
	acc.failed["fatal"] = true
	if err := r.executeTask(context.Background(), dependent, rt, dag, acc); err != nil {
		t.Fatalf("executeTask: %v", err)
	}
	if acc.skippedCount != 1 {
		t.Fatalf("expected 1 skipped task, got %d", acc.skippedCount)
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("RunLogSink.Close: %v", err)
	}

	validateRunLogFile(t, logPath)
}

// TestRunLogSchema_TargetUnreachableValidatesAgainstSchema exercises the
// target_unreachable path: a connection-establishment failure surfaced by
// target.Target.Info (classified via target.IsUnreachable) at apply
// start, before any task runs.
//
// The fixture error below is deliberately double-wrapped — an outer
// *preflighterr.TargetError{Op:"info"} (what remoteWindowsTargetInfo
// applies) around an inner one that actually carries
// preflighterr.ErrUnreachable — matching the real shape internal/target's
// wrapping code produces (see internal/target/target_errors_test.go for
// tests exercising that real wrapping code directly, including a real
// WinRM/SSH dial failure driven through the actual production call
// chain). This test's job is only to confirm apply.go correctly acts on
// target.IsUnreachable and that the resulting event stream validates
// against the schema, not to reprove the classifier itself.
func TestRunLogSchema_TargetUnreachableValidatesAgainstSchema(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	sink, err := output.NewRunLogSink("schema-run-unreachable", logPath)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	fake := &targettest.Fake{
		InfoErr: &preflighterr.TargetError{
			Transport: "winrm",
			Op:        "info",
			Err: &preflighterr.TargetError{
				Transport: "winrm",
				Op:        "create client",
				Err:       fmt.Errorf("%w: %w", preflighterr.ErrUnreachable, errors.New("dial tcp 10.0.0.5:5986: connect: connection refused")),
			},
		},
	}
	if !target.IsUnreachable(fake.InfoErr) {
		t.Fatalf("test fixture is broken: target.IsUnreachable(fake.InfoErr) = false")
	}
	r := New(fake, emptyResolver(), Config{Renderer: sink})
	plan := &ExecutionPlan{PlaybookName: "unreachable-target", Tasks: []*PlanTask{}}

	if err := r.apply(context.Background(), plan); err == nil {
		t.Fatal("expected apply to fail when the target is unreachable")
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("RunLogSink.Close: %v", err)
	}

	validateRunLogFile(t, logPath)

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"target_unreachable"`) {
		t.Fatalf("expected a target_unreachable event, got:\n%s", raw)
	}
}
