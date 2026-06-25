package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	ProjectName                   string
	ProjectEnv                    string
	ProjectVars                   map[string]any
	InventoryVars                 map[string]any
	Vars                          map[string]any // from --var CLI flags
	TargetVars                    map[string]any
	TargetName                    string
	Phase                         string // "plan", "fetch", "stage", "apply" (empty = all)
	SkipFetch                     bool
	Renderer                      output.Renderer
	Secrets                       *secrets.Resolver
	SecretsConfig                 config.SecretsConfig
	StatePath                     string
	ModuleRegistry                target.ModuleRegistry
	BundleOutputDir               string
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

func (r *Runner) closeTarget() error {
	if r.target == nil {
		return nil
	}
	if closer, ok := r.target.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Run executes the playbook through the configured phases.
// If Config.Phase is empty, all phases run in order: plan, fetch, stage, apply.
// Otherwise only the specified phase runs (plan is always required first).
func (r *Runner) Run(ctx context.Context, playbook *action.Playbook) (err error) {
	defer func() {
		err = errors.Join(err, r.closeTarget())
	}()

	if err := ctx.Err(); err != nil {
		return err
	}

	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.PlayStartEvent{PlayName: playbook.Name})
	}

	targetName := r.targetName()

	if r.config.Phase == "plan" {
		slog.Debug("starting phase", "phase", "plan")
		_, err := r.Plan(ctx, playbook)
		if err != nil {
			r.emitError(fmt.Errorf("plan phase failed: %w", err))
		}
		return err
	}

	if !r.config.SkipFetch {
		slog.Debug("starting phase", "phase", "fetch")
		if err := r.Fetch(ctx, playbook); err != nil {
			r.emitError(fmt.Errorf("fetch phase failed: %w", err))
			return err
		}
	}

	slog.Debug("starting phase", "phase", "plan")
	plan, err := r.Plan(ctx, playbook)
	if err != nil {
		r.emitError(fmt.Errorf("plan phase failed: %w", err))
		return err
	}

	if r.config.Phase == "fetch" {
		return nil
	}

	if r.config.Phase == "stage" {
		slog.Debug("starting phase", "phase", "stage")
		err := r.stage(ctx, plan)
		if err != nil {
			r.emitError(fmt.Errorf("stage phase failed: %w", err))
		}
		return err
	}

	// Emit target start before the apply phase.
	r.emitTargetStart(targetName)
	targetStartTime := time.Now()

	slog.Debug("starting phase", "phase", "apply")
	applyErr := r.apply(ctx, plan)

	// Emit target complete.
	elapsedMs := time.Since(targetStartTime).Milliseconds()
	r.emitTargetComplete(targetName, elapsedMs)

	if applyErr != nil {
		if !isApplyTaskFailureSummary(applyErr) {
			r.emitError(fmt.Errorf("apply phase failed: %w", applyErr))
		}
		return applyErr
	}

	return nil
}

func isApplyTaskFailureSummary(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "apply: ") && strings.Contains(err.Error(), " task(s) failed")
}

func (r *Runner) emitError(err error) {
	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.ErrorEvent{Message: err.Error()})
	}
}

// emitTargetStart emits a target-level start event.
func (r *Runner) emitTargetStart(targetName string) {
	if r.config.Renderer == nil {
		return
	}
	transport := "local"
	if r.target != nil {
		transport = string(r.target.Transport())
	}
	r.config.Renderer.Emit(output.TargetStartEvent{
		Target:    targetName,
		Transport: transport,
	})
}

// emitTargetComplete emits a target-level complete event.
func (r *Runner) emitTargetComplete(targetName string, elapsedMs int64) {
	if r.config.Renderer == nil {
		return
	}
	// Outcome is determined by whether any task failed.
	outcome := "ok"
	// Tracked via PlayEndEvent counters. For now default to ok.
	r.config.Renderer.Emit(output.TargetCompleteEvent{
		Target:    targetName,
		Outcome:   outcome,
		ElapsedMs: elapsedMs,
	})
}

func (r *Runner) emitTaskStart(pt *PlanTask) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.TaskStartEvent{
		TaskID:     pt.ID,
		TaskName:   pt.Name,
		ActionPath: pt.ActionPath,
		Target:     r.targetName(),
	})
}

func (r *Runner) emitWarning(message string) {
	if r.config.Renderer != nil && message != "" {
		r.config.Renderer.Emit(output.WarningEvent{Message: message})
	}
}

func (r *Runner) emitActivityStart(message string) {
	if r.config.Renderer == nil || !r.isRemoteTarget() {
		return
	}
	r.config.Renderer.Emit(output.ActivityStartEvent{
		Target:  r.targetName(),
		Message: message,
	})
}

func (r *Runner) emitActivityResult(message, status string) {
	if r.config.Renderer == nil || !r.isRemoteTarget() {
		return
	}
	r.config.Renderer.Emit(output.ActivityResultEvent{
		Target:  r.targetName(),
		Message: message,
		Status:  status,
	})
}

func (r *Runner) isRemoteTarget() bool {
	return r.target != nil && r.target.Transport() != target.TransportLocal
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

func (r *Runner) Fetch(ctx context.Context, playbook *action.Playbook) error {
	return r.fetch(ctx, playbook)
}

func (r *Runner) Plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
	return r.plan(ctx, playbook)
}

func (r *Runner) Stage(ctx context.Context, plan *ExecutionPlan) (err error) {
	defer func() { err = errors.Join(err, r.closeTarget()) }()
	return r.stage(ctx, plan)
}

func (r *Runner) Apply(ctx context.Context, plan *ExecutionPlan) (err error) {
	defer func() { err = errors.Join(err, r.closeTarget()) }()
	return r.apply(ctx, plan)
}
