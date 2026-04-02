package runner

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

// Config holds the options that control runner behavior.
type Config struct {
	DryRun                        bool
	Tags                          []string
	SkipTags                      []string
	Concurrency                   int
	ProjectDir                    string
	ProjectVars                   map[string]any
	InventoryVars                 map[string]any
	Vars                          map[string]any // from --var CLI flags
	TargetVars                    map[string]any
	TargetName                    string
	Phase                         string // "plan", "fetch", "stage", "apply" (empty = all)
	Renderer                      output.Renderer
	Secrets                       *secrets.Resolver
	SecretsConfig                 config.SecretsConfig
	StatePath                     string
	ModuleRegistry                target.ModuleRegistry
	BundleOutputDir               string
	BundleBinaryPath              string
	BundlePlugins                 []plugins.LoadedPlugin
	AllowPlaintextSecretsInBundle bool
	Lockfile                      *action.Lockfile
	Version                       string
	Commit                        string
	BuildDate                     string
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
	if err := ctx.Err(); err != nil {
		return err
	}

	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.Event{
			Type:     output.EventPlayStart,
			PlayName: playbook.Name,
		})
	}

	if r.config.Phase == "plan" {
		_, err := r.Plan(ctx, playbook)
		if err != nil {
			r.emitError(fmt.Errorf("plan phase failed: %w", err))
		}
		return err
	}

	if err := r.Fetch(ctx, playbook); err != nil {
		r.emitError(fmt.Errorf("fetch phase failed: %w", err))
		return err
	}

	plan, err := r.Plan(ctx, playbook)
	if err != nil {
		r.emitError(fmt.Errorf("plan phase failed: %w", err))
		return err
	}

	if r.config.Phase == "fetch" {
		return nil
	}

	if r.config.Phase == "stage" {
		err := r.Stage(ctx, plan)
		if err != nil {
			r.emitError(fmt.Errorf("stage phase failed: %w", err))
		}
		return err
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

func (r *Runner) emitTaskStart(pt *PlanTask) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:     output.EventTaskStart,
		TaskID:   pt.ID,
		TaskName: pt.Name,
		Target:   r.targetName(),
	})
}

func (r *Runner) emitWarning(message string) {
	if r.config.Renderer != nil && message != "" {
		r.config.Renderer.Emit(output.Event{
			Type:    output.EventWarning,
			Message: message,
		})
	}
}

// PlannedTaskState renders the current plan with execution-time target context
// so state comparisons use the same task names and params that apply records.
func (r *Runner) PlannedTaskState(ctx context.Context, plan *ExecutionPlan) ([]PlannedTaskState, error) {
	if r.target == nil {
		return nil, fmt.Errorf("state: target is not configured")
	}
	execCtx, err := r.buildExecutionContext(ctx)
	if err != nil {
		return nil, err
	}
	return BuildPlannedTaskState(ctx, plan, execCtx, r.config.Secrets)
}
