package runner

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/output"
)

// TestRun_PlanFailureStillTalliesTarget guards against TargetTallies
// undercounting a host that fails during the plan phase (before apply ever
// starts) — e.g. a DAG cycle, a template error, or (as here) a module the
// runtime support matrix rejects. Run must still emit a paired
// TargetStartEvent/TargetCompleteEvent so the host counts as failed instead
// of silently disappearing from run.json's tallies.
func TestRun_PlanFailureStillTalliesTarget(t *testing.T) {
	rec := &recordingRenderer{}
	r := New(&mockTarget{}, emptyResolver(), Config{
		TargetName: "kiosk-01",
		Renderer:   rec,
	})

	err := r.Run(context.Background(), singleTaskPlaybook("totally_made_up"))
	if err == nil {
		t.Fatal("expected Run to fail for an unresolvable module, got nil")
	}

	var starts, completes int
	var lastOutcome string
	for _, e := range rec.events {
		switch evt := e.(type) {
		case output.TargetStartEvent:
			starts++
		case output.TargetCompleteEvent:
			completes++
			lastOutcome = evt.Outcome
		}
	}
	if starts != 1 {
		t.Errorf("expected exactly 1 TargetStartEvent, got %d", starts)
	}
	if completes != 1 {
		t.Errorf("expected exactly 1 TargetCompleteEvent, got %d", completes)
	}
	if lastOutcome != "failed" {
		t.Errorf("expected TargetCompleteEvent outcome %q, got %q", "failed", lastOutcome)
	}

	// Confirm the same fold the TUI/text renderer and run.json use actually
	// counts this as a failed target, not an undercount.
	proj := output.NewRunProjection()
	for _, e := range rec.events {
		proj.Apply(e)
	}
	tallies := proj.Tallies()
	if tallies.Failed != 1 || tallies.OK != 0 {
		t.Errorf("Tallies() = %+v, want {OK:0 Failed:1}", tallies)
	}
}

// TestRun_ContextCanceledStillTalliesTarget covers the ctx.Err() early
// return: a run canceled before planning starts must still be counted as a
// failed target rather than vanishing from the tallies.
func TestRun_ContextCanceledStillTalliesTarget(t *testing.T) {
	rec := &recordingRenderer{}
	r := New(&mockTarget{}, emptyResolver(), Config{
		TargetName: "kiosk-02",
		Renderer:   rec,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Run(ctx, newShellPlaybook("canceled")); err == nil {
		t.Fatal("expected Run to fail for a canceled context, got nil")
	}

	proj := output.NewRunProjection()
	for _, e := range rec.events {
		proj.Apply(e)
	}
	tallies := proj.Tallies()
	if tallies.Failed != 1 || tallies.OK != 0 {
		t.Errorf("Tallies() = %+v, want {OK:0 Failed:1}", tallies)
	}
}
