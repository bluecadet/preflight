package runner

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

// Config holds the options that control runner behavior.
type Config struct {
	DryRun      bool
	Tags        []string
	SkipTags    []string
	Concurrency int
	ProjectVars map[string]any
	Vars        map[string]any // from --var CLI flags
	Phase       string         // "plan", "fetch", "stage", "apply" (empty = all)
	Renderer    output.Renderer
	Secrets     *secrets.Resolver
}

// Runner orchestrates the Plan→Fetch→Stage→Apply pipeline.
type Runner struct {
	target   target.Target
	resolver action.Chain
	config   Config
}

// New creates a new Runner with the given target, resolver chain, and config.
func New(t target.Target, resolver action.Chain, cfg Config) *Runner {
	return &Runner{
		target:   t,
		resolver: resolver,
		config:   cfg,
	}
}

// Run executes the playbook through the configured phases.
// If Config.Phase is empty, all phases run in order: plan, fetch, stage, apply.
// Otherwise only the specified phase runs (plan is always required first).
func (r *Runner) Run(ctx context.Context, playbook *action.Playbook) error {
	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.Event{
			Type:     output.EventPlayStart,
			PlayName: playbook.Name,
		})
	}

	plan, err := r.Plan(ctx, playbook)
	if err != nil {
		r.emitError(fmt.Errorf("plan phase failed: %w", err))
		return err
	}

	if r.config.Phase == "plan" {
		return nil
	}

	if err := r.Fetch(ctx, plan); err != nil {
		r.emitError(fmt.Errorf("fetch phase failed: %w", err))
		return err
	}

	if r.config.Phase == "fetch" {
		return nil
	}

	if err := r.Stage(ctx, plan); err != nil {
		r.emitError(fmt.Errorf("stage phase failed: %w", err))
		return err
	}

	if r.config.Phase == "stage" {
		return nil
	}

	if err := r.Apply(ctx, plan); err != nil {
		r.emitError(fmt.Errorf("apply phase failed: %w", err))
		return err
	}

	return nil
}

func (r *Runner) emitError(err error) {
	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.Event{
			Type:  output.EventError,
			Error: err,
		})
	}
}
