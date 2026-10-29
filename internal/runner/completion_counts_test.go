package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/preflighterr"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
)

// Counts describe task outcomes, not target failures or tasks that were never
// reached. Keep them consistent with the task-event fold used by run_summary.
func assertCompletionCounts(t *testing.T, events []output.Event, want [4]int, outcome string) {
	t.Helper()
	var completes []output.TargetCompleteEvent
	projection := output.NewRunProjection()
	for _, event := range events {
		projection.Apply(event)
		if complete, ok := event.(output.TargetCompleteEvent); ok {
			completes = append(completes, complete)
		}
	}
	if len(completes) != 1 {
		t.Fatalf("got %d completion events, want 1", len(completes))
	}
	complete := completes[0]
	got := [4]int{complete.OKCount, complete.ChangedCount, complete.FailedCount, complete.SkippedCount}
	if got != want {
		t.Errorf("completion counts [ok changed failed skipped] = %v, want %v", got, want)
	}
	folded := [4]int{projection.OkCount, projection.ChangedCount, projection.FailedCount, projection.SkippedCount}
	if got != folded {
		t.Errorf("completion counts = %v, task-event counts = %v", got, folded)
	}
	if complete.Outcome != outcome {
		t.Errorf("completion outcome = %q, want %q", complete.Outcome, outcome)
	}
}

func completionTask(name string) action.Task {
	return action.Task{Name: name, InlineModules: map[string]map[string]any{"shell": {"cmd": "echo test"}}}
}

func TestRunCompletionCounts(t *testing.T) {
	tagged := completionTask("tagged out")
	tagged.Tags = []string{"excluded"}
	conditional := completionTask("when false")
	conditional.When = "false"
	ignored := completionTask("ignored failure")
	ignored.IgnoreErrors = true
	badBinding := completionTask("bad binding")
	badBinding.InlineModules["shell"]["cmd"] = "{{ vars.missing }}"
	badWhen := completionTask("bad when")
	badWhen.When = "{{ vars.missing }}"

	for _, tt := range []struct {
		name    string
		tasks   []action.Task
		results []target.Result
		execErr error
		want    [4]int
		wantErr bool
	}{
		{
			name: "live QA totals",
			tasks: []action.Task{completionTask("one"), completionTask("two"), completionTask("three"),
				completionTask("four"), completionTask("five"), completionTask("six")},
			results: []target.Result{{Status: target.StatusOK}, {Status: target.StatusOK},
				{Status: target.StatusChanged}, {Status: target.StatusChanged},
				{Status: target.StatusChanged}, {Status: target.StatusChanged}},
			want: [4]int{2, 4, 0, 0},
		},
		{
			name:    "all skip sources",
			tasks:   []action.Task{tagged, conditional, completionTask("module skipped"), completionTask("changed")},
			results: []target.Result{{Status: target.StatusSkipped}, {Status: target.StatusChanged}},
			want:    [4]int{0, 1, 0, 3},
		},
		{
			name:    "fail fast does not count unreached tasks",
			tasks:   []action.Task{completionTask("changed"), completionTask("failed"), completionTask("unreached")},
			results: []target.Result{{Status: target.StatusChanged}, {Status: target.StatusFailed}},
			want:    [4]int{0, 1, 1, 0}, wantErr: true,
		},
		{
			name:    "execution error",
			tasks:   []action.Task{completionTask("failed"), completionTask("unreached")},
			execErr: errors.New("execution failed"),
			want:    [4]int{0, 0, 1, 0}, wantErr: true,
		},
		{
			name:    "ignored execution error counts ok before later failure",
			tasks:   []action.Task{ignored, completionTask("also ok")},
			execErr: errors.New("ignored execution failure"),
			want:    [4]int{1, 0, 1, 0}, wantErr: true,
		},
		{
			name:    "ignored failed status continues but still counts failed",
			tasks:   []action.Task{ignored, completionTask("changed")},
			results: []target.Result{{Status: target.StatusFailed}, {Status: target.StatusChanged}},
			want:    [4]int{0, 1, 1, 0}, wantErr: true,
		},
		{
			name:    "binding error preserves earlier counts without inventing task failure",
			tasks:   []action.Task{completionTask("changed"), badBinding},
			results: []target.Result{{Status: target.StatusChanged}},
			want:    [4]int{0, 1, 0, 0}, wantErr: true,
		},
		{
			name:  "when error preserves earlier counts",
			tasks: []action.Task{completionTask("ok"), badWhen},
			want:  [4]int{1, 0, 0, 0}, wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingRenderer{}
			fake := &targettest.Fake{Results: tt.results, ExecErr: tt.execErr}
			r := New(fake, emptyResolver(), Config{Renderer: rec, TargetName: "host", SkipTags: []string{"excluded"}})
			err := r.Run(context.Background(), &action.Playbook{Name: tt.name, Tasks: tt.tasks})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			outcome := "ok"
			if tt.wantErr {
				outcome = "failed"
			}
			assertCompletionCounts(t, rec.events, tt.want, outcome)
		})
	}
}

func TestRunCompletionCountsBeforeTasks(t *testing.T) {
	for _, tt := range []struct {
		name   string
		module string
		fake   *targettest.Fake
		cancel bool
	}{
		{name: "plan failure", module: "not_a_module", fake: &targettest.Fake{}},
		{name: "canceled before plan", module: "shell", fake: &targettest.Fake{}, cancel: true},
		{name: "unreachable", module: "shell", fake: &targettest.Fake{InfoErr: fmt.Errorf("%w: dial failed", preflighterr.ErrUnreachable)}},
		{name: "target info failure", module: "shell", fake: &targettest.Fake{InfoErr: errors.New("info failed")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingRenderer{}
			r := New(tt.fake, emptyResolver(), Config{Renderer: rec})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			if err := r.Run(ctx, singleTaskPlaybook(tt.module)); err == nil {
				t.Fatal("Run() succeeded, want pre-task failure")
			}
			assertCompletionCounts(t, rec.events, [4]int{}, "failed")
		})
	}
}

// A panic after a successful task must not discard the partial counts.
type completionPanicTarget struct {
	targettest.Fake
}

func (f *completionPanicTarget) Execute(ctx context.Context, taskID, module string, params map[string]any, dryRun bool, onOutput target.OutputFunc) (target.Result, error) {
	if len(f.Calls) > 0 {
		panic("second task panicked")
	}
	return f.Fake.Execute(ctx, taskID, module, params, dryRun, onOutput)
}

func TestRunCompletionCountsSurvivePanic(t *testing.T) {
	rec := &recordingRenderer{}
	fake := &completionPanicTarget{Fake: targettest.Fake{Results: []target.Result{{Status: target.StatusChanged}}}}
	r := New(fake, emptyResolver(), Config{Renderer: rec})
	pb := &action.Playbook{Tasks: []action.Task{completionTask("changed"), completionTask("panic")}}
	if err := r.Run(context.Background(), pb); err == nil {
		t.Fatal("Run() succeeded, want recovered panic")
	}
	assertCompletionCounts(t, rec.events, [4]int{0, 1, 0, 0}, "failed")
}

func TestRunCompletionCountsAcrossTargetsAndActions(t *testing.T) {
	rec := &recordingRenderer{}
	renderer := output.Synchronized(rec)
	resolver := action.Chain{&staticResolver{action: &action.Action{
		Name: "pair", Tasks: []action.Task{completionTask("first"), completionTask("second")},
	}}}
	pb := &action.Playbook{Tasks: []action.Task{{Name: "action one", Uses: "pair"}, {Name: "action two", Uses: "pair"}}}
	var wg sync.WaitGroup
	for _, host := range []struct {
		name   string
		status target.Status
	}{
		{name: "unchanged", status: target.StatusOK},
		{name: "changed", status: target.StatusChanged},
	} {
		wg.Go(func() {
			fake := &targettest.Fake{Results: []target.Result{{Status: host.status}}}
			r := New(fake, resolver, Config{Renderer: renderer, TargetName: host.name})
			// Reusing a runner must start fresh rather than retain prior counts.
			for range 2 {
				if err := r.Run(context.Background(), pb); err != nil {
					t.Errorf("Run(%s): %v", host.name, err)
				}
			}
		})
	}
	wg.Wait()
	completes := 0
	for _, event := range rec.events {
		if complete, ok := event.(output.TargetCompleteEvent); ok {
			completes++
			want := [4]int{4, 0, 0, 0}
			if complete.Target == "changed" {
				want = [4]int{0, 4, 0, 0}
			}
			got := [4]int{complete.OKCount, complete.ChangedCount, complete.FailedCount, complete.SkippedCount}
			if got != want {
				t.Errorf("%s completion counts = %v, want %v", complete.Target, got, want)
			}
		}
	}
	if completes != 4 {
		t.Errorf("got %d completion events, want 4", completes)
	}
}
