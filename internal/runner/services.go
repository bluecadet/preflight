package runner

import (
	"context"

	"github.com/bluecadet/preflight/internal/action"
)

// Fetcher handles the fetch phase for a runner.
type Fetcher struct {
	runner *Runner
}

// Planner handles the plan phase for a runner.
type Planner struct {
	runner *Runner
}

// Stager handles the stage phase for a runner.
type Stager struct {
	runner *Runner
}

// Executor handles the apply phase for a runner.
type Executor struct {
	runner *Runner
}

// NewFetcher returns the fetch-phase service for this runner.
func (r *Runner) NewFetcher() *Fetcher {
	return &Fetcher{runner: r}
}

// NewPlanner returns the plan-phase service for this runner.
func (r *Runner) NewPlanner() *Planner {
	return &Planner{runner: r}
}

// NewStager returns the stage-phase service for this runner.
func (r *Runner) NewStager() *Stager {
	return &Stager{runner: r}
}

// NewExecutor returns the apply-phase service for this runner.
func (r *Runner) NewExecutor() *Executor {
	return &Executor{runner: r}
}

// Fetch downloads remote action refs not yet in cache.
func (r *Runner) Fetch(ctx context.Context, playbook *action.Playbook) error {
	return r.NewFetcher().Fetch(ctx, playbook)
}

// Plan resolves all action refs, expands tasks into a flat list, resolves
// variables, and returns an execution plan.
func (r *Runner) Plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
	return r.NewPlanner().Plan(ctx, playbook)
}

// Stage assembles a self-contained artifact bundle (zip).
func (r *Runner) Stage(ctx context.Context, plan *ExecutionPlan) error {
	return r.NewStager().Stage(ctx, plan)
}

// Apply executes the task graph against the target.
func (r *Runner) Apply(ctx context.Context, plan *ExecutionPlan) error {
	return r.NewExecutor().Apply(ctx, plan)
}

// Fetch runs the fetch phase.
func (f *Fetcher) Fetch(ctx context.Context, playbook *action.Playbook) error {
	return f.runner.fetch(ctx, playbook)
}

// Plan runs the plan phase.
func (p *Planner) Plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
	return p.runner.plan(ctx, playbook)
}

// Stage runs the stage phase.
func (s *Stager) Stage(ctx context.Context, plan *ExecutionPlan) error {
	return s.runner.stage(ctx, plan)
}

// Apply runs the apply phase.
func (e *Executor) Apply(ctx context.Context, plan *ExecutionPlan) error {
	return e.runner.apply(ctx, plan)
}
