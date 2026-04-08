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
	"github.com/bluecadet/preflight/internal/bundle"
	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/stdlib"
	"github.com/bluecadet/preflight/internal/target"
)

type mockTarget struct {
	results []target.Result // in order; last result is reused if list is exhausted
	calls   []mockCall
	output  []string
	execErr error
}

type mockCall struct {
	TaskID string
	Module string
	DryRun bool
	Params map[string]any
}

func (m *mockTarget) Execute(_ context.Context, taskID, module string, params map[string]any, _ target.ExecutionOptions, dryRun bool, onOutput target.OutputFunc) (target.Result, error) {
	var copied map[string]any
	if params != nil {
		copied = make(map[string]any, len(params))
		maps.Copy(copied, params)
	}
	m.calls = append(m.calls, mockCall{TaskID: taskID, Module: module, DryRun: dryRun, Params: copied})
	if onOutput != nil {
		for _, line := range m.output {
			onOutput(line)
		}
	}
	if m.execErr != nil {
		return target.Result{TaskID: taskID, Output: append([]string(nil), m.output...)}, m.execErr
	}
	if len(m.results) == 0 {
		return target.Result{TaskID: taskID, Status: target.StatusOK, Output: append([]string(nil), m.output...)}, nil
	}
	idx := len(m.calls) - 1
	if idx >= len(m.results) {
		idx = len(m.results) - 1
	}
	r := m.results[idx]
	r.TaskID = taskID
	if len(m.output) > 0 && len(r.Output) == 0 {
		r.Output = append([]string(nil), m.output...)
	}
	return r, nil
}

func (m *mockTarget) Info(_ context.Context) (target.TargetInfo, error) {
	return target.TargetInfo{}, nil
}

type recordingRenderer struct {
	events []output.Event
}

func (r *recordingRenderer) Emit(e output.Event) { r.events = append(r.events, e) }
func (r *recordingRenderer) Close()              {}

type emptyChain struct{}

func (emptyChain) Resolve(_ context.Context, ref string) (*action.Action, error) { return nil, nil }
func (emptyChain) Name() string                                                  { return "empty" }

func emptyResolver() action.Chain {
	return action.Chain{emptyChain{}}
}

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
	if plan.Tasks[0].ID == "" {
		t.Fatal("expected stable task ID to be populated")
	}
	if strings.Contains(plan.Tasks[0].ID, "task-0") {
		t.Fatalf("expected non-positional task ID, got %q", plan.Tasks[0].ID)
	}
}

func TestPlanMergesProjectVarsAndActionInputs(t *testing.T) {
	resolver := action.Chain{&staticResolver{
		action: &action.Action{
			Name: "preflight/autologin",
			Inputs: map[string]action.Input{
				"username": {Required: true},
				"password": {},
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
					"username": "kiosk",
					"password": "secret:autologin-password",
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
	preview, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask returned error: %v", err)
	}
	values, ok := preview.Params["values"].(map[string]any)
	if !ok {
		t.Fatalf("expected values map, got %T", preview.Params["values"])
	}
	if values["DefaultPassword"] != "secret:autologin-password" {
		t.Fatalf("expected secret ref to be preserved in plan, got %#v", values["DefaultPassword"])
	}
	if values["Site"] != "Lobby" {
		t.Fatalf("expected project var to flow into action rendering, got %#v", values["Site"])
	}
}

func TestPlanMergesBecomeDefaultsAcrossActionExpansion(t *testing.T) {
	resolver := action.Chain{&staticResolver{
		action: &action.Action{
			Name: "acme/demo",
			Defaults: action.TaskDefaults{
				Become: map[string]any{"method": "sudo"},
			},
			Tasks: []action.Task{
				{
					Name:  "echo",
					Shell: map[string]any{"cmd": "echo", "args": []any{"hello"}},
				},
			},
		},
	}}
	r := New(&mockTarget{}, resolver, Config{})
	pb := &action.Playbook{
		Name: "test",
		Defaults: action.TaskDefaults{
			Become: map[string]any{"user": "playbook-user"},
		},
		Tasks: []action.Task{
			{
				Name:   "call action",
				Uses:   "acme/demo",
				Become: map[string]any{"user": "task-user"},
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
	if got := plan.Tasks[0].Become["user"]; got != "task-user" {
		t.Fatalf("expected task become user override, got %#v", got)
	}
	if got := plan.Tasks[0].Become["method"]; got != "sudo" {
		t.Fatalf("expected action default method, got %#v", got)
	}
	if got := plan.Tasks[0].Become["enabled"]; got != true {
		t.Fatalf("expected become enabled by default, got %#v", got)
	}
}

func TestPlanTaskBecomeCanDisableInheritedDefaults(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{})
	pb := &action.Playbook{
		Name: "test",
		Defaults: action.TaskDefaults{
			Become: map[string]any{"user": "playbook-user"},
		},
		Tasks: []action.Task{
			{
				Name:   "echo",
				Become: map[string]any{"enabled": false},
				Shell:  map[string]any{"cmd": "echo", "args": []any{"hello"}},
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
	if got := plan.Tasks[0].Become["enabled"]; got != false {
		t.Fatalf("expected become disabled override, got %#v", got)
	}
	if _, ok := plan.Tasks[0].Become["user"]; ok {
		t.Fatalf("expected disabled override to clear inherited become fields, got %#v", plan.Tasks[0].Become)
	}
	if len(plan.Tasks[0].Become) != 1 {
		t.Fatalf("expected disabled override to keep only enabled=false, got %#v", plan.Tasks[0].Become)
	}
}

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

func TestDAGCycleDetection(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "task-a", DependsOn: []string{"task-b"}}
	taskB := &PlanTask{ID: "task-1", Name: "task-b", DependsOn: []string{"task-a"}}

	_, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestPlanStdlibWindowsMachineRendersLeafInputs(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{
		ProjectVars: map[string]any{
			"device_name": "Gallery-Kiosk-01",
			"device_tz":   "Eastern Standard Time",
		},
	})
	pb := &action.Playbook{
		Name: "windows-machine",
		Tasks: []action.Task{
			{
				Name: "machine baseline",
				Uses: "preflight/windows-machine",
				With: map[string]any{
					"computer_name": "{{ vars.device_name }}",
					"timezone":      "{{ vars.device_tz }}",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 4 {
		t.Fatalf("expected 4 expanded tasks, got %d", len(plan.Tasks))
	}

	previewName, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask(computer name) returned error: %v", err)
	}
	checkScript, ok := previewName.Params["check_script"].(string)
	if !ok {
		t.Fatalf("expected check_script string, got %T", previewName.Params["check_script"])
	}
	if !strings.Contains(checkScript, "Gallery-Kiosk-01") {
		t.Fatalf("expected rendered computer name in check script, got:\n%s", checkScript)
	}

	previewTZ, err := PreviewTask(plan.Tasks[1], nil)
	if err != nil {
		t.Fatalf("PreviewTask(timezone) returned error: %v", err)
	}
	tzScript, ok := previewTZ.Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", previewTZ.Params["script"])
	}
	if !strings.Contains(tzScript, "Eastern Standard Time") {
		t.Fatalf("expected rendered timezone in script, got:\n%s", tzScript)
	}
}

func TestPlanStdlibWindowsPowerRendersTemplatedSettingsLists(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{})
	pb := &action.Playbook{
		Name: "windows-power",
		Tasks: []action.Task{
			{
				Name: "power baseline",
				Uses: "preflight/windows-power",
				With: map[string]any{
					"plan_name":              "Exhibit Plan",
					"display_timeout_ac":     5,
					"display_timeout_dc":     7,
					"sleep_timeout_ac":       0,
					"sleep_timeout_dc":       10,
					"scheduled_reboot_state": "present",
					"scheduled_reboot_time":  "04:30",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected 3 expanded tasks, got %d", len(plan.Tasks))
	}

	previewPlan, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask(power plan) returned error: %v", err)
	}
	if previewPlan.Module != "power_plan" {
		t.Fatalf("expected power_plan task, got %q", previewPlan.Module)
	}
	settings, ok := previewPlan.Params["settings"].([]any)
	if !ok {
		t.Fatalf("expected settings list, got %T", previewPlan.Params["settings"])
	}
	if len(settings) != 2 {
		t.Fatalf("expected 2 settings, got %d", len(settings))
	}
	first, ok := settings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first setting map, got %T", settings[0])
	}
	if first["ac_value"] != "5" || first["dc_value"] != "7" {
		t.Fatalf("unexpected first setting values: %#v", first)
	}

	previewReboot, err := PreviewTask(plan.Tasks[2], nil)
	if err != nil {
		t.Fatalf("PreviewTask(scheduled reboot) returned error: %v", err)
	}
	if previewReboot.Params["ensure"] != "present" {
		t.Fatalf("expected scheduled task ensure=present, got %#v", previewReboot.Params["ensure"])
	}
	if previewReboot.Params["start_at"] != "04:30" {
		t.Fatalf("expected scheduled reboot time 04:30, got %#v", previewReboot.Params["start_at"])
	}
}

func TestApplyStdlibWindowsMachineRendersNestedExecutionTimeInputs(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	mt := &mockTarget{
		results: []target.Result{
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
		},
	}
	r := New(mt, resolver, Config{
		TargetVars: map[string]any{
			"hostname": "Gallery-Kiosk-01",
			"timezone": "Eastern Standard Time",
		},
	})
	pb := &action.Playbook{
		Name: "windows-machine",
		Tasks: []action.Task{
			{
				Name: "machine baseline",
				Uses: "preflight/windows-machine",
				With: map[string]any{
					"computer_name": "{{ target.hostname }}",
					"timezone":      "{{ target.timezone }}",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(mt.calls) != 4 {
		t.Fatalf("expected 4 executed tasks, got %d", len(mt.calls))
	}

	checkScript, ok := mt.calls[0].Params["check_script"].(string)
	if !ok {
		t.Fatalf("expected check_script string, got %T", mt.calls[0].Params["check_script"])
	}
	if strings.Contains(checkScript, "{{") {
		t.Fatalf("expected rendered computer name script, got:\n%s", checkScript)
	}
	if !strings.Contains(checkScript, "Gallery-Kiosk-01") {
		t.Fatalf("expected rendered computer name in check script, got:\n%s", checkScript)
	}

	tzScript, ok := mt.calls[1].Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", mt.calls[1].Params["script"])
	}
	if strings.Contains(tzScript, "{{") {
		t.Fatalf("expected rendered timezone script, got:\n%s", tzScript)
	}
	if !strings.Contains(tzScript, "Eastern Standard Time") {
		t.Fatalf("expected rendered timezone in script, got:\n%s", tzScript)
	}
}

func TestApplyStdlibWindowsMachineSkipsOptionalTasksWhenInputsOmitted(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	mt := &mockTarget{
		results: []target.Result{
			{Status: target.StatusOK},
			{Status: target.StatusOK},
		},
	}
	r := New(mt, resolver, Config{})
	pb := &action.Playbook{
		Name: "windows-machine",
		Tasks: []action.Task{
			{
				Name: "machine baseline",
				Uses: "preflight/windows-machine",
				With: map[string]any{},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	// computer_name and timezone tasks should be skipped; only long_paths and
	// ps1_execution_policy should execute.
	if len(mt.calls) != 2 {
		t.Fatalf("expected 2 executed tasks (skipped computer_name and timezone), got %d", len(mt.calls))
	}
}

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

	var taskResults, playEnds int
	for _, e := range rec.events {
		switch e.(type) {
		case output.TaskResultEvent:
			taskResults++
		case output.PlayEndEvent:
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

func TestApplyEmitsTaskOutputEventsWithTargetContext(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusChanged}},
		output:  []string{"line1", "line2"},
	}
	rec := &recordingRenderer{}
	cfg := Config{
		Renderer:   rec,
		TargetName: "gallery-01",
	}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("emit-output-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var taskOutputs []output.TaskOutputEvent
	for _, e := range rec.events {
		if toe, ok := e.(output.TaskOutputEvent); ok {
			taskOutputs = append(taskOutputs, toe)
		}
	}
	if len(taskOutputs) != 2 {
		t.Fatalf("expected 2 task_output events, got %d", len(taskOutputs))
	}
	for i, e := range taskOutputs {
		if e.Target != "gallery-01" {
			t.Fatalf("task_output[%d] target = %q, want %q", i, e.Target, "gallery-01")
		}
		if e.TaskID == "" {
			t.Fatalf("task_output[%d] missing task id", i)
		}
		if e.TaskName != "run echo" {
			t.Fatalf("task_output[%d] task name = %q, want %q", i, e.TaskName, "run echo")
		}
		if len(e.Lines) != 1 || e.Lines[0] != mt.output[i] {
			t.Fatalf("task_output[%d] lines = %v, want [%q]", i, e.Lines, mt.output[i])
		}
	}
}

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
				"password": "secret:autologin-password",
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

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{
		LastApplied: time.Now().Truncate(time.Second),
		Tasks:       make(map[string]TaskSnapshot),
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

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}

	if len(loaded.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded.Tasks))
	}
	got := loaded.Tasks["task-0"]
	if got.TaskKey != "task-0" {
		t.Errorf("task key mismatch: got %q", got.TaskKey)
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

func TestLoadStateMissing(t *testing.T) {
	s, err := LoadState("/nonexistent/path/state.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil State")
	}
	if len(s.Tasks) != 0 {
		t.Errorf("expected empty Tasks, got %d", len(s.Tasks))
	}
}

func TestComparePlannedTasksNoStateMarksAllNew(t *testing.T) {
	comparisons := ComparePlannedTasks([]PlannedTaskState{
		{TaskKey: "task-a", TaskName: "one", ParamHash: "a", TaskHash: "hash-a"},
		{TaskKey: "task-b", TaskName: "two", ParamHash: "b", TaskHash: "hash-b"},
	}, &State{Tasks: map[string]TaskSnapshot{}})

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
		Tasks: map[string]TaskSnapshot{
			"task-0": {TaskKey: "task-0", TaskName: "known task", ParamHash: "same", TaskHash: "same", Status: target.StatusOK},
			"task-1": {TaskKey: "task-1", TaskName: "changed task", ParamHash: "old", TaskHash: "old", Status: target.StatusChanged},
			"task-2": {TaskKey: "task-2", TaskName: "removed task", ParamHash: "gone", TaskHash: "gone", Status: target.StatusFailed},
		},
	}

	comparisons := ComparePlannedTasks([]PlannedTaskState{
		{TaskKey: "task-0", TaskName: "known task", ParamHash: "same", TaskHash: "same"},
		{TaskKey: "task-1", TaskName: "changed task", ParamHash: "new", TaskHash: "new"},
	}, state)

	if len(comparisons) != 3 {
		t.Fatalf("expected 3 comparisons, got %d", len(comparisons))
	}
	if comparisons[0].Status != ComparisonStatusUnchanged {
		t.Fatalf("expected first comparison to be UNCHANGED, got %s", comparisons[0].Status)
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

func TestBuildPlannedTaskStateRendersExecutionTimeTemplates(t *testing.T) {
	plan := &ExecutionPlan{
		PlaybookName: "rendered-state",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "echo {{ target.name }} on {{ facts.os.name }}",
			Module: "shell",
			Params: map[string]any{
				"cmd":  "echo",
				"args": []any{"{{ env.SITE }}", "{{ target.address }}", "{{ facts.os.build }}"},
			},
			TemplateVars: map[string]any{},
		}},
	}

	planned, err := BuildPlannedTaskState(context.Background(), plan, &executionContext{
		target: map[string]any{
			"name":    "kiosk-a",
			"address": "10.0.0.1",
		},
		facts: map[string]any{
			"os": map[string]any{
				"name":  "Windows 11",
				"build": 22631,
			},
		},
		env: map[string]string{
			"SITE": "lobby",
		},
	}, nil)
	if err != nil {
		t.Fatalf("BuildPlannedTaskState returned error: %v", err)
	}
	if len(planned) != 1 {
		t.Fatalf("expected 1 planned task, got %d", len(planned))
	}
	if planned[0].TaskName != "echo kiosk-a on Windows 11" {
		t.Fatalf("expected rendered task name, got %q", planned[0].TaskName)
	}
	wantHash := ParamHash(map[string]any{
		"cmd":  "echo",
		"args": []any{"lobby", "10.0.0.1", "22631"},
	})
	if planned[0].ParamHash != wantHash {
		t.Fatalf("expected rendered param hash %q, got %q", wantHash, planned[0].ParamHash)
	}
}

func TestBuildPlannedTaskStateResolvesDependenciesByRawTaskName(t *testing.T) {
	plan := &ExecutionPlan{
		PlaybookName: "dep-state",
		Tasks: []*PlanTask{
			{
				ID:           "task-0",
				Name:         "prepare {{ target.name }}",
				Module:       "shell",
				Params:       map[string]any{"cmd": "echo"},
				TemplateVars: map[string]any{},
			},
			{
				ID:           "task-1",
				Name:         "apply",
				Module:       "shell",
				DependsOn:    []string{"prepare {{ target.name }}"},
				Params:       map[string]any{"cmd": "echo"},
				TemplateVars: map[string]any{},
			},
		},
	}

	planned, err := BuildPlannedTaskState(context.Background(), plan, &executionContext{
		target: map[string]any{"name": "kiosk-a"},
	}, nil)
	if err != nil {
		t.Fatalf("BuildPlannedTaskState returned error: %v", err)
	}
	if len(planned) != 2 {
		t.Fatalf("expected 2 planned tasks, got %d", len(planned))
	}
	if len(planned[1].DependsOn) != 1 || planned[1].DependsOn[0] != "task-0" {
		t.Fatalf("expected dependency on task-0, got %#v", planned[1].DependsOn)
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
	recorded := state.Tasks["task-0"]
	if recorded.ParamHash == "" {
		t.Fatal("expected saved state to include param hash")
	}
	if recorded.ParamHash != ParamHash(plan.Tasks[0].Params) {
		t.Fatalf("expected param hash %q, got %q", ParamHash(plan.Tasks[0].Params), recorded.ParamHash)
	}
}

func TestRunFetchAndStagePhases(t *testing.T) {
	playbook := newShellPlaybook("phase-test")
	if err := New(&mockTarget{}, emptyResolver(), Config{Phase: "fetch"}).Run(context.Background(), playbook); err != nil {
		t.Fatalf("fetch phase: expected nil, got %v", err)
	}

	dir := t.TempDir()
	err := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
	}).Run(context.Background(), playbook)
	if err != nil {
		t.Fatalf("stage phase: expected nil, got %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		t.Fatalf("Glob bundle output: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one staged bundle, got %d", len(matches))
	}
}

func TestStageBundlesReferencedEncryptedSecrets(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"db-password": {File: "secrets/db-password.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("db-password", []byte("hunter2")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
		ProjectDir:      dir,
		SecretsConfig:   cfg,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "bundle-secret",
		Vars:         map[string]any{},
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set env secret",
			Module: "shell",
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "secret:db-password",
				},
			},
		}},
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	bundlePath := mustOneBundlePath(t, dir)
	extracted, err := bundle.Extract(bundlePath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}()

	if extracted.Manifest.SecretMode != bundle.SecretModeEncrypted {
		t.Fatalf("expected encrypted secret mode, got %q", extracted.Manifest.SecretMode)
	}
	if len(extracted.Manifest.SecretEntries) != 1 || extracted.Manifest.SecretEntries[0].Name != "db-password" {
		t.Fatalf("unexpected secret entries: %#v", extracted.Manifest.SecretEntries)
	}
	planBytes, err := os.ReadFile(extracted.PlanPath)
	if err != nil {
		t.Fatalf("ReadFile(plan): %v", err)
	}
	if !strings.Contains(string(planBytes), "secret:db-password") {
		t.Fatalf("expected staged plan to preserve secret ref, got %q", string(planBytes))
	}
	if strings.Contains(string(planBytes), "hunter2") {
		t.Fatalf("expected staged plan to avoid plaintext secret, got %q", string(planBytes))
	}
	encryptedBytes, err := os.ReadFile(filepath.Join(dir, "secrets", "db-password.age"))
	if err != nil {
		t.Fatalf("ReadFile(secret source): %v", err)
	}
	bundledBytes, err := os.ReadFile(filepath.Join(extracted.RootDir, filepath.FromSlash(extracted.Manifest.SecretEntries[0].Path)))
	if err != nil {
		t.Fatalf("ReadFile(bundled secret): %v", err)
	}
	if string(bundledBytes) != string(encryptedBytes) {
		t.Fatalf("expected bundled ciphertext to match source ciphertext")
	}
}

func TestStageRejectsLiteralSecretWithoutPlaintextFlag(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: t.TempDir(),
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
	})
	plan := &ExecutionPlan{
		PlaybookName: "literal-secret",
		Vars:         map[string]any{},
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set literal secret",
			Module: "shell",
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "hunter2",
				},
			},
		}},
	}

	err := r.Stage(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "cannot be embedded in a staged bundle") {
		t.Fatalf("expected literal secret stage failure, got %v", err)
	}
}

func TestStageAllowsPlaintextSecretsWhenFlagEnabled(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"db-password": {File: "secrets/db-password.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("db-password", []byte("hunter2")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:                         "stage",
		BundleOutputDir:               dir,
		ModuleRegistry:                map[string]target.Module{"shell": noopModule{}},
		ProjectDir:                    dir,
		SecretsConfig:                 cfg,
		AllowPlaintextSecretsInBundle: true,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "plaintext-secret",
		Vars:         map[string]any{},
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set secret env",
			Module: "shell",
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "secret:db-password",
					"TOKEN":    "abc123",
				},
			},
		}},
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	bundlePath := mustOneBundlePath(t, dir)
	extracted, err := bundle.Extract(bundlePath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}()

	if extracted.Manifest.SecretMode != bundle.SecretModePlaintext {
		t.Fatalf("expected plaintext secret mode, got %q", extracted.Manifest.SecretMode)
	}
	if len(extracted.Manifest.SecretEntries) != 1 {
		t.Fatalf("expected one bundled plaintext secret, got %#v", extracted.Manifest.SecretEntries)
	}
	info, err := os.Stat(extracted.PlanPath)
	if err != nil {
		t.Fatalf("Stat(plan): %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected plaintext plan mode 0600, got %#o", info.Mode().Perm())
	}
	secretBytes, err := os.ReadFile(filepath.Join(extracted.RootDir, filepath.FromSlash(extracted.Manifest.SecretEntries[0].Path)))
	if err != nil {
		t.Fatalf("ReadFile(secret): %v", err)
	}
	if string(secretBytes) != "hunter2" {
		t.Fatalf("expected plaintext bundled secret, got %q", string(secretBytes))
	}
}

func mustOneBundlePath(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one staged bundle, got %d", len(matches))
	}
	return matches[0]
}

type noopModule struct{}

func (noopModule) Check(_ context.Context, _ map[string]any) (bool, error) { return false, nil }
func (noopModule) Apply(_ context.Context, _ map[string]any) error         { return nil }

type fetchableResolver struct {
	actions map[string]*action.Action
	fetched map[string]bool
	calls   []string
}

func (r *fetchableResolver) Name() string { return "fetchable" }

func (r *fetchableResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	if !r.fetched[ref] {
		return nil, &action.RemoteCacheMissError{Ref: ref}
	}
	return a, nil
}

func (r *fetchableResolver) Fetch(_ context.Context, ref string) (*action.FetchResult, error) {
	r.calls = append(r.calls, ref)
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	if r.fetched == nil {
		r.fetched = make(map[string]bool)
	}
	r.fetched[ref] = true
	return &action.FetchResult{
		Entry:  action.LockEntry{Ref: ref, SHA: "sha-" + ref, Pinned: ref},
		Action: a,
	}, nil
}

func TestRunFetchesRemoteDependenciesBeforePlanningAndApply(t *testing.T) {
	rootRef := "github.com/acme/actions/root@v1"
	childRef := "github.com/acme/actions/child@v1"
	resolver := &fetchableResolver{
		actions: map[string]*action.Action{
			rootRef: {
				Name: "root",
				Tasks: []action.Task{{
					Name: "child",
					Uses: childRef,
				}},
			},
			childRef: {
				Name: "child",
				Tasks: []action.Task{{
					Name:  "echo",
					Shell: map[string]any{"cmd": "echo hello"},
				}},
			},
		},
		fetched: make(map[string]bool),
	}

	playbook := &action.Playbook{
		Name: "remote",
		Tasks: []action.Task{{
			Name: "root",
			Uses: rootRef,
		}},
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, action.Chain{resolver}, Config{})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("expected 2 fetch calls, got %d", len(resolver.calls))
	}
	if resolver.calls[0] != rootRef || resolver.calls[1] != childRef {
		t.Fatalf("unexpected fetch order: %#v", resolver.calls)
	}
	if len(mt.calls) != 1 || mt.calls[0].Module != "shell" {
		t.Fatalf("expected one shell execution, got %#v", mt.calls)
	}
}

func TestRunFetchPhaseStopsBeforeApply(t *testing.T) {
	rootRef := "github.com/acme/actions/root@v1"
	resolver := &fetchableResolver{
		actions: map[string]*action.Action{
			rootRef: {
				Name: "root",
				Tasks: []action.Task{{
					Name:  "echo",
					Shell: map[string]any{"cmd": "echo hello"},
				}},
			},
		},
		fetched: make(map[string]bool),
	}

	playbook := &action.Playbook{
		Name:  "remote",
		Tasks: []action.Task{{Name: "root", Uses: rootRef}},
	}
	mt := &mockTarget{}
	r := New(mt, action.Chain{resolver}, Config{Phase: "fetch"})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run(fetch): %v", err)
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("expected 1 fetch call, got %d", len(resolver.calls))
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no target execution during fetch phase, got %#v", mt.calls)
	}
}

// localActionResolver handles a fixed set of non-remote refs without any fetch step,
// simulating a local or embedded action resolver.
type localActionResolver struct {
	actions map[string]*action.Action
}

func (r *localActionResolver) Name() string { return "local" }
func (r *localActionResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func TestRunFetchesRemoteDepsNestedUnderLocalAction(t *testing.T) {
	localRef := "local/wrapper"
	remoteRef := "github.com/acme/actions/child@v1"

	localAction := &action.Action{
		Name:  "wrapper",
		Tasks: []action.Task{{Name: "child", Uses: remoteRef}},
	}
	remoteAction := &action.Action{
		Name: "child",
		Tasks: []action.Task{{
			Name:  "echo",
			Shell: map[string]any{"cmd": "echo hello"},
		}},
	}

	fr := &fetchableResolver{
		actions: map[string]*action.Action{remoteRef: remoteAction},
		fetched: make(map[string]bool),
	}
	chain := action.Chain{
		&localActionResolver{actions: map[string]*action.Action{localRef: localAction}},
		fr,
	}

	playbook := &action.Playbook{
		Name: "local-to-remote",
		Tasks: []action.Task{{
			Name: "wrapper",
			Uses: localRef,
		}},
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, chain, Config{})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fr.calls) != 1 || fr.calls[0] != remoteRef {
		t.Errorf("expected fetch of %q, got %v", remoteRef, fr.calls)
	}
	if len(mt.calls) != 1 || mt.calls[0].Module != "shell" {
		t.Errorf("expected one shell execution, got %#v", mt.calls)
	}
}

func TestRunFetchesRemoteDepsNestedUnderStdlibAction(t *testing.T) {
	stdlibRef := "preflight/baseline"
	remoteRef := "github.com/acme/actions/plugin@v3"

	stdlibAction := &action.Action{
		Name:  "baseline",
		Tasks: []action.Task{{Name: "plugin", Uses: remoteRef}},
	}
	remoteAction := &action.Action{
		Name: "plugin",
		Tasks: []action.Task{{
			Name:  "run",
			Shell: map[string]any{"cmd": "plugin.exe"},
		}},
	}

	fr := &fetchableResolver{
		actions: map[string]*action.Action{remoteRef: remoteAction},
		fetched: make(map[string]bool),
	}
	chain := action.Chain{
		&localActionResolver{actions: map[string]*action.Action{stdlibRef: stdlibAction}},
		fr,
	}

	playbook := &action.Playbook{
		Name: "stdlib-to-remote",
		Tasks: []action.Task{{
			Name: "baseline",
			Uses: stdlibRef,
		}},
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, chain, Config{})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fr.calls) != 1 || fr.calls[0] != remoteRef {
		t.Errorf("expected fetch of %q, got %v", remoteRef, fr.calls)
	}
	if len(mt.calls) != 1 || mt.calls[0].Module != "shell" {
		t.Errorf("expected one shell execution, got %#v", mt.calls)
	}
}

func TestRunPlanPhaseStopsBeforeApply(t *testing.T) {
	mt := &mockTarget{}
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.json")

	r := New(mt, emptyResolver(), Config{
		Phase:     "plan",
		StatePath: statePath,
	})
	pb := newShellPlaybook("plan-only")
	if err := r.Run(context.Background(), pb); err != nil {
		t.Fatalf("Run(plan): %v", err)
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no target execution during plan phase, got %#v", mt.calls)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no state file to be written during plan phase, but stat returned: %v", err)
	}
}

func TestTargetNameNeverReturnsLocalhost(t *testing.T) {
	// When no TargetName is configured and TargetVars is empty, targetName()
	// must not return "localhost" — that would silently claim a local identity.
	r := &Runner{config: Config{}}
	got := r.targetName()
	if got == "localhost" {
		t.Errorf("targetName() returned %q with no config; expected empty string or another sentinel, not %q", got, "localhost")
	}

	// Verify it returns empty string specifically.
	if got != "" {
		t.Errorf("targetName() = %q, want %q when unconfigured", got, "")
	}
}

func TestTargetNamePrefersExplicitConfig(t *testing.T) {
	r := &Runner{config: Config{TargetName: "exhibit-01"}}
	if got := r.targetName(); got != "exhibit-01" {
		t.Errorf("targetName() = %q, want %q", got, "exhibit-01")
	}
}

func TestTargetNameFallsBackToTargetVars(t *testing.T) {
	r := &Runner{config: Config{TargetVars: map[string]any{"name": "kiosk-pc"}}}
	if got := r.targetName(); got != "kiosk-pc" {
		t.Errorf("targetName() = %q, want %q", got, "kiosk-pc")
	}

	r2 := &Runner{config: Config{TargetVars: map[string]any{"hostname": "gallery-01"}}}
	if got := r2.targetName(); got != "gallery-01" {
		t.Errorf("targetName() = %q, want %q", got, "gallery-01")
	}
}

func TestNewTaskSnapshotNilDAGProducesEmptyDependsOn(t *testing.T) {
	pt := &PlanTask{
		ID:        "task-1",
		Name:      "dependent task",
		Module:    "shell",
		DependsOn: []string{"failing task"},
		Params:    map[string]any{"cmd": "echo"},
	}

	snap := newTaskSnapshot(pt, pt.Name, pt.Params, pt.Params, nil, nil, target.StatusSkipped, "dependency-failed", nil)
	if len(snap.DependsOn) != 0 {
		t.Fatalf("expected empty DependsOn when dag=nil, got %v", snap.DependsOn)
	}
	if snap.Status != target.StatusSkipped {
		t.Fatalf("expected StatusSkipped, got %v", snap.Status)
	}
	if snap.Message != "dependency-failed" {
		t.Fatalf("expected message %q, got %q", "dependency-failed", snap.Message)
	}
}

func TestNewTaskSnapshotWithDAGResolvesDependencyIDs(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "failing task", Module: "shell"}
	taskB := &PlanTask{
		ID:        "task-1",
		Name:      "dependent task",
		Module:    "shell",
		DependsOn: []string{"failing task"},
		Params:    map[string]any{"cmd": "echo"},
	}

	dag, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err != nil {
		t.Fatalf("BuildDAG error: %v", err)
	}

	snap := newTaskSnapshot(taskB, taskB.Name, taskB.Params, taskB.Params, nil, nil, target.StatusOK, "", dag)
	if len(snap.DependsOn) != 1 || snap.DependsOn[0] != "task-0" {
		t.Fatalf("expected DependsOn=[task-0], got %v", snap.DependsOn)
	}
}
