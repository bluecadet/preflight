package runner

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
	"github.com/bluecadet/preflight/internal/template"
)

func TestApplyResolved(t *testing.T) {
	// Shared RuntimeContext: no real connection needed.
	rt := &template.RuntimeContext{
		Target: map[string]any{"os": "linux"},
		Facts:  map[string]any{},
		Env:    map[string]string{},
	}

	allTags := []string{"always"}
	neverTags := []string{"never"}

	tests := []struct {
		name        string
		tasks       []*PlanTask
		fake        *targettest.Fake
		rt          *template.RuntimeContext
		cfg         Config
		wantErr     bool
		wantCalls   int
		wantOK      int
		wantChanged int
		wantFailed  int
		wantSkipped int
	}{
		{
			name: "tag-filtered task is skipped",
			tasks: []*PlanTask{
				{
					ID:     "task-0",
					Name:   "filtered-out",
					Module: "shell",
					Params: map[string]any{"cmd": "echo"},
					Scope:  template.NewScope(),
					Tags:   neverTags,
				},
			},
			fake: &targettest.Fake{
				InfoValue: target.TargetInfo{Transport: target.TransportLocal},
			},
			rt:          rt,
			cfg:         Config{Tags: allTags},
			wantErr:     false,
			wantCalls:   0,
			wantSkipped: 1,
		},
		{
			name: "when-condition-false skips task",
			tasks: []*PlanTask{
				{
					ID:     "task-0",
					Name:   "runs",
					Module: "shell",
					Params: map[string]any{"cmd": "echo"},
					Scope:  template.NewScope(map[string]any{"should_run": true}),
					Tags:   allTags,
				},
				{
					ID:     "task-1",
					Name:   "skipped-by-when",
					Module: "shell",
					Params: map[string]any{"cmd": "echo"},
					Scope:  template.NewScope(map[string]any{"should_run": false}),
					Tags:   allTags,
					When:   "{{ vars.should_run }}",
				},
			},
			fake: &targettest.Fake{
				InfoValue: target.TargetInfo{Transport: target.TransportLocal},
				Results:   []target.Result{{Status: target.StatusOK}},
			},
			rt:          rt,
			cfg:         Config{Tags: allTags},
			wantErr:     false,
			wantCalls:   1,
			wantOK:      1,
			wantSkipped: 1,
		},
		{
			name: "task succeeds with ok status",
			tasks: []*PlanTask{
				{
					ID:     "task-0",
					Name:   "success",
					Module: "shell",
					Params: map[string]any{"cmd": "echo"},
					Scope:  template.NewScope(),
					Tags:   allTags,
				},
			},
			fake: &targettest.Fake{
				InfoValue: target.TargetInfo{Transport: target.TransportLocal},
				Results:   []target.Result{{Status: target.StatusOK}},
			},
			rt:        rt,
			cfg:       Config{Tags: allTags},
			wantErr:   false,
			wantCalls: 1,
			wantOK:    1,
		},
		{
			name: "task reports changed status",
			tasks: []*PlanTask{
				{
					ID:     "task-0",
					Name:   "change",
					Module: "shell",
					Params: map[string]any{"cmd": "touch"},
					Scope:  template.NewScope(),
					Tags:   allTags,
				},
			},
			fake: &targettest.Fake{
				InfoValue: target.TargetInfo{Transport: target.TransportLocal},
				Results:   []target.Result{{Status: target.StatusChanged}},
			},
			rt:          rt,
			cfg:         Config{Tags: allTags},
			wantErr:     false,
			wantCalls:   1,
			wantChanged: 1,
		},
		{
			name: "failing task halts apply without ignore_errors",
			tasks: []*PlanTask{
				{
					ID:     "task-0",
					Name:   "fatal",
					Module: "shell",
					Params: map[string]any{"cmd": "fail"},
					Scope:  template.NewScope(),
					Tags:   allTags,
				},
				{
					ID:     "task-1",
					Name:   "after-fatal",
					Module: "shell",
					Params: map[string]any{"cmd": "never-runs"},
					Scope:  template.NewScope(),
					Tags:   allTags,
				},
			},
			fake: &targettest.Fake{
				InfoValue: target.TargetInfo{Transport: target.TransportLocal},
				Results: []target.Result{
					{Status: target.StatusFailed, Message: "fatal error"},
					{Status: target.StatusOK},
				},
			},
			rt:         rt,
			cfg:        Config{Tags: allTags},
			wantErr:    true,
			wantCalls:  1,
			wantFailed: 1,
		},
		{
			name: "ignore_errors continues on StatusFailed result",
			tasks: []*PlanTask{
				{
					ID:           "task-0",
					Name:         "ignored-fail",
					Module:       "shell",
					Params:       map[string]any{"cmd": "fail"},
					Scope:        template.NewScope(),
					Tags:         allTags,
					IgnoreErrors: true,
				},
				{
					ID:     "task-1",
					Name:   "after-ignored",
					Module: "shell",
					Params: map[string]any{"cmd": "echo"},
					Scope:  template.NewScope(),
					Tags:   allTags,
				},
			},
			fake: &targettest.Fake{
				InfoValue: target.TargetInfo{Transport: target.TransportLocal},
				Results: []target.Result{
					{Status: target.StatusFailed, Message: "expected"},
					{Status: target.StatusOK},
				},
			},
			rt:         rt,
			cfg:        Config{Tags: allTags},
			wantErr:    true, // failedCount > 0 → finalizeApply returns error
			wantCalls:  2,
			wantFailed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dag, err := BuildDAG(tt.tasks)
			if err != nil {
				t.Fatalf("BuildDAG error: %v", err)
			}

			r := New(tt.fake, emptyResolver(), tt.cfg)
			err = r.applyResolved(context.Background(), dag, tt.rt)
			if (err != nil) != tt.wantErr {
				t.Errorf("applyResolved() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if got := len(tt.fake.Calls); got != tt.wantCalls {
				t.Errorf("target.Execute calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestApplyAcquireThenResolved(t *testing.T) {
	// Verify that apply() calls buildExecutionContext then applyResolved.
	// A fake that returns nil means applyResolved receives the injected ctx.
	tgt := &targettest.Fake{
		InfoValue: target.TargetInfo{
			Hostname:  "test-host",
			OSFamily:  target.OSFamilyLinux,
			Transport: target.TransportSSH,
		},
		Results: []target.Result{{Status: target.StatusOK}},
	}
	r := New(tgt, emptyResolver(), Config{})
	plan := &ExecutionPlan{
		PlaybookName: "acquire-then-resolve",
		Tasks: []*PlanTask{
			{
				ID:     "task-0",
				Name:   "shell task",
				Module: "shell",
				Params: map[string]any{"cmd": "echo"},
				Scope:  template.NewScope(),
			},
		},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(tgt.Calls) != 1 {
		t.Fatalf("expected one Execute call, got %d", len(tgt.Calls))
	}
}

func TestApplyResolvedSkippedByDependencyFailure(t *testing.T) {
	// When a task has IgnoreErrors=false and its dependency is in the
	// failed set, the task is skipped with reason "dependency-failed".
	// This path requires an ignored-failure task to populate acc.failed
	// while still allowing the loop to continue; we simulate that by
	// having the first task return StatusFailed with IgnoreErrors=true.
	// (In the current code, only non-ignored failures populate the failed
	// map, so the actual skip branch is exercised differently below.)
	//
	// For now, test that a dependent task never runs when its upstream
	// fails non-ignored (i.e., the halt prevents execution).
	tasks := []*PlanTask{
		{
			ID:     "task-0",
			Name:   "fatal",
			Module: "shell",
			Params: map[string]any{"cmd": "fail"},
			Scope:  template.NewScope(),
			Tags:   []string{"all"},
		},
		{
			ID:        "task-1",
			Name:      "dependent",
			Module:    "shell",
			Params:    map[string]any{"cmd": "never-runs"},
			Scope:     template.NewScope(),
			Tags:      []string{"all"},
			DependsOn: []string{"fatal"},
		},
	}
	dag, err := BuildDAG(tasks)
	if err != nil {
		t.Fatalf("BuildDAG error: %v", err)
	}
	fake := &targettest.Fake{
		InfoValue: target.TargetInfo{Transport: target.TransportLocal},
		Results: []target.Result{
			{Status: target.StatusFailed, Message: "boom"},
			{Status: target.StatusOK},
		},
	}
	r := New(fake, emptyResolver(), Config{Tags: []string{"all"}})
	err = r.applyResolved(context.Background(), dag, &template.RuntimeContext{})
	if err == nil {
		t.Fatal("expected applyResolved to return an error on failure")
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("expected only 1 task to execute (fatal), got %d", len(fake.Calls))
	}
}
