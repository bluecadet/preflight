package runner

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

// ---- Helpers ----------------------------------------------------------------

// mockTarget is a Target that records calls and returns a preset status.
type mockTarget struct {
	results []target.Result // in order; last result is reused if list is exhausted
	calls   []mockCall
	execErr error
}

type mockCall struct {
	TaskID string
	Module string
	DryRun bool
	Params map[string]any
}

func (m *mockTarget) Execute(_ context.Context, taskID, module string, params map[string]any, dryRun bool) (target.Result, error) {
	var copied map[string]any
	if params != nil {
		copied = make(map[string]any, len(params))
		maps.Copy(copied, params)
	}
	m.calls = append(m.calls, mockCall{TaskID: taskID, Module: module, DryRun: dryRun, Params: copied})
	if m.execErr != nil {
		return target.Result{}, m.execErr
	}
	if len(m.results) == 0 {
		return target.Result{TaskID: taskID, Status: target.StatusOK}, nil
	}
	idx := len(m.calls) - 1
	if idx >= len(m.results) {
		idx = len(m.results) - 1
	}
	r := m.results[idx]
	r.TaskID = taskID
	return r, nil
}

func (m *mockTarget) CopyFile(_ context.Context, _, _ string) error        { return nil }
func (m *mockTarget) ReadFile(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (m *mockTarget) Reachable(_ context.Context) (bool, error)            { return true, nil }
func (m *mockTarget) Info(_ context.Context) (target.TargetInfo, error) {
	return target.TargetInfo{}, nil
}

// recordingRenderer captures events for assertions.
type recordingRenderer struct {
	events []output.Event
}

func (r *recordingRenderer) Emit(e output.Event) { r.events = append(r.events, e) }
func (r *recordingRenderer) Close()              {}

// emptyChain is a resolver chain that never finds anything (for playbooks with
// no action refs).
type emptyChain struct{}

func (emptyChain) Resolve(_ context.Context, ref string) (*action.Action, error) { return nil, nil }
func (emptyChain) Name() string                                                  { return "empty" }

func emptyResolver() action.Chain {
	return action.Chain{emptyChain{}}
}

// newShellPlaybook builds a minimal playbook with a single shell task.
func newShellPlaybook(name string) *action.Playbook {
	return &action.Playbook{
		Name: name,
		Tasks: []action.Task{
			{
				Name:  "run echo",
				Shell: map[string]any{"cmd": "echo hello"},
			},
		},
	}
}

func ageGenerateIdentity(dir string) (*age.X25519Identity, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "keys.txt"), []byte(identity.String()+"\n"), 0o600); err != nil {
		return nil, err
	}
	return identity, nil
}

// ---- Tests ------------------------------------------------------------------

// 1. Plan phase: parse a simple playbook with one shell task, verify ExecutionPlan.
func TestPlanSingleTask(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{})
	pb := newShellPlaybook("test play")

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if plan == nil {
		t.Fatal("Plan returned nil plan")
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Module != "shell" {
		t.Errorf("expected module %q, got %q", "shell", plan.Tasks[0].Module)
	}
	if plan.Tasks[0].ID != "task-0" {
		t.Errorf("expected ID %q, got %q", "task-0", plan.Tasks[0].ID)
	}
}

func TestPlanMergesProjectVarsAndActionInputs(t *testing.T) {
	resolver := action.Chain{&staticResolver{
		action: &action.Action{
			Name: "preflight/autologin",
			Inputs: map[string]action.Input{
				"username":      {Required: true},
				"password_from": {},
			},
			Tasks: []action.Task{
				{
					Name: "configure autologin",
					Registry: map[string]any{
						"path": "HKLM:\\Software\\Winlogon",
						"values": map[string]any{
							"DefaultUserName": "{{ vars.username }}",
							"DefaultPassword": "{{ vars.password }}",
							"Site":            "{{ vars.site }}",
						},
					},
				},
			},
		},
	}}
	r := New(&mockTarget{}, resolver, Config{
		ProjectVars: map[string]any{"site": "Lobby"},
	})
	pb := &action.Playbook{
		Name: "test",
		Tasks: []action.Task{
			{
				Name: "autologin",
				Uses: "preflight/autologin",
				With: map[string]any{
					"username":      "kiosk",
					"password_from": "secret:autologin-password",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	values, ok := plan.Tasks[0].Params["values"].(map[string]any)
	if !ok {
		t.Fatalf("expected values map, got %T", plan.Tasks[0].Params["values"])
	}
	if values["DefaultPassword"] != "secret:autologin-password" {
		t.Fatalf("expected secret ref to be preserved in plan, got %#v", values["DefaultPassword"])
	}
	if values["Site"] != "Lobby" {
		t.Fatalf("expected project var to flow into action rendering, got %#v", values["Site"])
	}
}

// 2. DAG: two tasks where B depends_on A — TopologicalOrder puts A before B.
func TestDAGDependsOnOrder(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "task-a"}
	taskB := &PlanTask{ID: "task-1", Name: "task-b", DependsOn: []string{"task-a"}}

	dag, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err != nil {
		t.Fatalf("BuildDAG error: %v", err)
	}

	ordered := dag.TopologicalOrder()
	if len(ordered) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(ordered))
	}
	if ordered[0].ID != "task-0" || ordered[1].ID != "task-1" {
		t.Errorf("wrong order: got %v, want [task-0, task-1]", []string{ordered[0].ID, ordered[1].ID})
	}
}

// 3. DAG: circular dependency returns error.
func TestDAGCycleDetection(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "task-a", DependsOn: []string{"task-b"}}
	taskB := &PlanTask{ID: "task-1", Name: "task-b", DependsOn: []string{"task-a"}}

	_, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

// 4. Apply: mock target that always returns StatusOK — renderer receives task_result events.
func TestApplyEmitsTaskResultEvents(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusOK}},
	}
	rec := &recordingRenderer{}
	cfg := Config{Renderer: rec}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("emit-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	// Expect at least one task_result event and a play_end event.
	var taskResults, playEnds int
	for _, e := range rec.events {
		switch e.Type {
		case output.EventTaskResult:
			taskResults++
		case output.EventPlayEnd:
			playEnds++
		}
	}
	if taskResults != 1 {
		t.Errorf("expected 1 task_result event, got %d", taskResults)
	}
	if playEnds != 1 {
		t.Errorf("expected 1 play_end event, got %d", playEnds)
	}
}

// 5. Apply with dryRun=true — verify target.Execute called with dryRun=true.
func TestApplyDryRun(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusChanged}},
	}
	cfg := Config{DryRun: true}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("dry-run-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if len(mt.calls) == 0 {
		t.Fatal("expected Execute to be called, got 0 calls")
	}
	if !mt.calls[0].DryRun {
		t.Error("expected dryRun=true in Execute call")
	}
}

func TestApplyResolvesSecretsBeforeExecute(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}

	provider := secrets.NewRepoProvider(dir, config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"autologin-password": {File: "secrets/autologin-password.age"},
		},
	})
	if err := provider.Encrypt("autologin-password", []byte("top-secret")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, emptyResolver(), Config{
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "secret-test",
		Vars:         map[string]any{},
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set secret",
			Module: "shell",
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "secret:autologin-password",
				},
				"password_from": "secret:autologin-password",
			},
		}},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(mt.calls) != 1 {
		t.Fatalf("expected one Execute call, got %d", len(mt.calls))
	}
	if mt.calls[0].Params["password"] != "top-secret" {
		t.Fatalf("expected password param to be resolved, got %#v", mt.calls[0].Params["password"])
	}
	env, ok := mt.calls[0].Params["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %T", mt.calls[0].Params["env"])
	}
	if env["PASSWORD"] != "top-secret" {
		t.Fatalf("expected nested secret ref to resolve, got %#v", env["PASSWORD"])
	}
}

// 6. State: Record + Save + Load round-trip.
func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{
		LastApplied: time.Now().Truncate(time.Second),
		Results:     make(map[string]TaskResult),
	}
	s.Record(TaskResult{
		TaskID:    "task-0",
		TaskName:  "run echo",
		Status:    target.StatusOK,
		Timestamp: time.Now().Truncate(time.Second),
		ParamHash: "abc123",
	})

	if err := s.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}

	if len(loaded.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(loaded.Results))
	}
	got := loaded.Results["task-0"]
	if got.TaskID != "task-0" {
		t.Errorf("task ID mismatch: got %q", got.TaskID)
	}
	if got.Status != target.StatusOK {
		t.Errorf("status mismatch: got %q", got.Status)
	}
	if got.ParamHash != "abc123" {
		t.Errorf("param hash mismatch: got %q", got.ParamHash)
	}
}

type staticResolver struct {
	action *action.Action
}

func (r *staticResolver) Resolve(_ context.Context, _ string) (*action.Action, error) {
	return r.action, nil
}
func (r *staticResolver) Name() string { return "static" }

// 7. LoadState returns empty State when file is missing (not an error).
func TestLoadStateMissing(t *testing.T) {
	s, err := LoadState("/nonexistent/path/state.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil State")
	}
	if len(s.Results) != 0 {
		t.Errorf("expected empty Results, got %d", len(s.Results))
	}
}

func TestComparePlannedTasksNoStateMarksAllNew(t *testing.T) {
	comparisons := ComparePlannedTasks([]PlannedTaskState{
		{TaskID: "task-0", TaskName: "one", ParamHash: "a"},
		{TaskID: "task-1", TaskName: "two", ParamHash: "b"},
	}, &State{Results: map[string]TaskResult{}})

	if len(comparisons) != 2 {
		t.Fatalf("expected 2 comparisons, got %d", len(comparisons))
	}
	for i, comparison := range comparisons {
		if comparison.Status != ComparisonStatusNew {
			t.Fatalf("comparison %d: expected NEW, got %s", i, comparison.Status)
		}
	}
}

func TestComparePlannedTasksMarksKnownChangedAndRemoved(t *testing.T) {
	state := &State{
		Results: map[string]TaskResult{
			"task-0": {TaskID: "task-0", TaskName: "known task", ParamHash: "same", Status: target.StatusOK},
			"task-1": {TaskID: "task-1", TaskName: "changed task", ParamHash: "old", Status: target.StatusChanged},
			"task-2": {TaskID: "task-2", TaskName: "removed task", ParamHash: "gone", Status: target.StatusFailed},
		},
	}

	comparisons := ComparePlannedTasks([]PlannedTaskState{
		{TaskID: "task-0", TaskName: "known task", ParamHash: "same"},
		{TaskID: "task-1", TaskName: "changed task", ParamHash: "new"},
	}, state)

	if len(comparisons) != 3 {
		t.Fatalf("expected 3 comparisons, got %d", len(comparisons))
	}
	if comparisons[0].Status != ComparisonStatusKnown {
		t.Fatalf("expected first comparison to be KNOWN, got %s", comparisons[0].Status)
	}
	if comparisons[1].Status != ComparisonStatusChanged {
		t.Fatalf("expected second comparison to be CHANGED, got %s", comparisons[1].Status)
	}
	if comparisons[2].Status != ComparisonStatusRemoved {
		t.Fatalf("expected third comparison to be REMOVED, got %s", comparisons[2].Status)
	}
	if comparisons[2].RecordedStatus != target.StatusFailed {
		t.Fatalf("expected removed task to keep recorded status, got %s", comparisons[2].RecordedStatus)
	}
}

func TestApplySavesStateWithParamHashes(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "provision.json")
	mt := &mockTarget{results: []target.Result{{Status: target.StatusChanged}}}
	r := New(mt, emptyResolver(), Config{StatePath: statePath})
	plan := &ExecutionPlan{
		PlaybookName: "state-save",
		Vars:         map[string]any{},
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "shell task",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
		}},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	recorded := state.Results["task-0"]
	if recorded.ParamHash == "" {
		t.Fatal("expected saved state to include param hash")
	}
	if recorded.ParamHash != ParamHash(plan.Tasks[0].Params) {
		t.Fatalf("expected param hash %q, got %q", ParamHash(plan.Tasks[0].Params), recorded.ParamHash)
	}
}

func TestRunFetchAndStagePhasesReturnNotImplemented(t *testing.T) {
	playbook := newShellPlaybook("phase-test")
	for _, phase := range []string{"fetch", "stage"} {
		r := New(&mockTarget{}, emptyResolver(), Config{Phase: phase})
		err := r.Run(context.Background(), playbook)
		if err == nil {
			t.Fatalf("phase %q: expected error, got nil", phase)
		}
		if !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("phase %q: expected not implemented error, got %v", phase, err)
		}
	}
}
