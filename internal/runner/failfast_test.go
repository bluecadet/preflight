package runner

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/template"
)

func TestApplyStopsOnFirstNonIgnoredFailure(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{
			{Status: target.StatusFailed, Message: "boom"},
			{Status: target.StatusOK},
		},
	}
	r := New(mt, emptyResolver(), Config{})
	plan := &ExecutionPlan{
		PlaybookName: "fail-fast",
		Tasks: []*PlanTask{
			{
				ID:     "task-0",
				Name:   "first",
				Module: "shell",
				Params: map[string]any{"cmd": "echo"},
				Scope:  template.NewScope(),
			},
			{
				ID:     "task-1",
				Name:   "second",
				Module: "shell",
				Params: map[string]any{"cmd": "echo"},
				Scope:  template.NewScope(),
			},
		},
	}

	err := r.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected Apply to fail")
	}
	if len(mt.calls) != 1 {
		t.Fatalf("expected execution to stop after the first failing task, got %d calls", len(mt.calls))
	}
}

func TestApplyIgnoreErrorsContinuesToLaterTasks(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{
			{Status: target.StatusFailed, Message: "boom"},
			{Status: target.StatusOK},
		},
	}
	r := New(mt, emptyResolver(), Config{})
	plan := &ExecutionPlan{
		PlaybookName: "ignore-errors",
		Tasks: []*PlanTask{
			{
				ID:           "task-0",
				Name:         "first",
				Module:       "shell",
				Params:       map[string]any{"cmd": "echo"},
				IgnoreErrors: true,
				Scope:        template.NewScope(),
			},
			{
				ID:     "task-1",
				Name:   "second",
				Module: "shell",
				Params: map[string]any{"cmd": "echo"},
				Scope:  template.NewScope(),
			},
		},
	}

	err := r.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected Apply to still report the ignored failure in the final result")
	}
	if len(mt.calls) != 2 {
		t.Fatalf("expected ignore_errors task failure to allow later tasks to run, got %d calls", len(mt.calls))
	}
}
