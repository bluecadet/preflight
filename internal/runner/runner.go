package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
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
	DryRun        bool
	Tags          []string
	SkipTags      []string
	ProjectDir    string
	ProjectName   string
	ProjectEnv    string
	ProjectVars   map[string]any
	InventoryVars map[string]any
	Vars          map[string]any // from --var CLI flags
	TargetVars    map[string]any
	TargetName    string
	// StagePlatform overrides live target discovery only while assembling a bundle.
	StagePlatform                 *target.Platform
	Renderer                      output.Renderer
	Secrets                       *secrets.Resolver
	SecretsConfig                 config.SecretsConfig
	ModuleRegistry                target.ModuleRegistry
	BundleOutputDir               string
	BundlePlugins                 []plugins.LoadedPlugin
	AllowPlaintextSecretsInBundle bool
	Lockfile                      *action.Lockfile
	Version                       string
	Commit                        string
	BuildDate                     string
}

// Runner orchestrates the Plan→Stage→Apply pipeline.
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

// Run plans and applies the playbook. Remote action refs must already be
// fetched by the caller.
func (r *Runner) Run(ctx context.Context, playbook *action.Playbook) (err error) {
	defer func() {
		err = errors.Join(err, r.closeTarget())
	}()

	targetName := r.targetName()

	// Run is committed to the tallied pipeline: every
	// return path must leave exactly one TargetStartEvent paired with
	// exactly one TargetCompleteEvent, since TargetTallies (and the
	// TUI/text renderer's live target roster) is folded purely from that
	// pair. The normal pair is emitted just before apply, further down. A
	// failure before that point (cancellation or Plan) would
	// otherwise return with no target event at all, silently dropping the
	// host from run.json's tallies — so planFailed emits the pair itself.
	if err := ctx.Err(); err != nil {
		return r.planFailed(targetName, err)
	}

	slog.Debug("starting phase", "phase", "plan")
	plan, err := r.Plan(ctx, playbook)
	if err != nil {
		slog.Error("plan phase failed", "error", err)
		return r.planFailed(targetName, err)
	}

	// Emit target start before the apply phase.
	r.emitTargetStart(targetName)
	targetStartTime := time.Now()

	// Track whether apply had failures for the target completion event.
	var hasFailure bool

	slog.Debug("starting phase", "phase", "apply")
	applyErr := r.applyRecovered(ctx, plan)
	if applyErr != nil {
		hasFailure = true
	}

	// Emit target complete.
	elapsedMs := time.Since(targetStartTime).Milliseconds()
	reason := ""
	if applyErr != nil && !isApplyTaskFailureSummary(applyErr) {
		reason = failureReason(applyErr)
	}
	r.emitTargetComplete(targetName, elapsedMs, hasFailure, reason)

	if applyErr != nil {
		if !isApplyTaskFailureSummary(applyErr) {
			slog.Error("apply phase failed", "error", applyErr)
		}
		return applyErr
	}

	return nil
}

// applyRecovered runs the apply phase and converts a panic into an error, so
// a single misbehaving module, plugin, or transport cannot crash the whole
// process. The recovered failure flows through the exact same path as any
// other apply error below: hasFailure is set, emitTargetComplete records the
// target as "failed" (preserving the TargetStartEvent/TargetCompleteEvent
// pairing the rest of Run depends on), and the error is returned to the
// caller for the run summary and exit status.
func (r *Runner) applyRecovered(ctx context.Context, plan *ExecutionPlan) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("apply: panic: %v\n%s", rec, debug.Stack())
		}
	}()
	return r.apply(ctx, plan)
}

// failureReason returns the first line of err, so a recovered panic's stack
// does not land in the recap.
func failureReason(err error) string {
	first, _, _ := strings.Cut(err.Error(), "\n")
	return first
}

func isApplyTaskFailureSummary(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "apply: ") && strings.Contains(err.Error(), " task(s) failed")
}

// emitTargetStart emits a target-level start event.
func (r *Runner) emitTargetStart(targetName string) {
	transport := "local"
	if r.target != nil {
		transport = string(r.target.Transport())
	}
	r.emit(output.TargetStartEvent{
		Target:    targetName,
		Transport: transport,
	})
}

// emitTargetComplete emits a target-level complete event.
func (r *Runner) emitTargetComplete(targetName string, elapsedMs int64, hasFailure bool, reason string) {
	outcome := "ok"
	if hasFailure {
		outcome = "failed"
	}
	var winrmRoundTrips int64
	if counter, ok := r.target.(target.RoundTripCounter); ok {
		winrmRoundTrips = counter.RoundTripCount()
	}
	r.emit(output.TargetCompleteEvent{
		Target:          targetName,
		Outcome:         outcome,
		Error:           reason,
		ElapsedMs:       elapsedMs,
		WinRMRoundTrips: winrmRoundTrips,
	})
}

// planFailed emits a synthetic TargetStartEvent/TargetCompleteEvent pair for
// a target that failed before reaching the apply phase (context
// cancellation or Plan — including DAG cycles, template errors, and
// validatePlanTasks rejections), then returns cause unchanged. Without this
// pair, the failure would return with no target event at all: TargetTallies
// is folded purely from TargetStart/TargetComplete, so the host would
// silently vanish from run.json's tallies instead of counting as failed.
// Callers must only use this on early-return paths that precede the normal
// emitTargetStart/emitTargetComplete pair further down in Run — never
// alongside it — or the target would be double-counted.
func (r *Runner) planFailed(targetName string, cause error) error {
	r.emitTargetStart(targetName)
	r.emitTargetComplete(targetName, 0, true, failureReason(cause))
	return cause
}

func (r *Runner) emitWarning(message string) {
	if message != "" {
		r.emit(output.WarningEvent{Message: message})
	}
}

func (r *Runner) emitActivityStart(message string) {
	if !r.isRemoteTarget() {
		return
	}
	r.emit(output.ActivityStartEvent{
		Target:  r.targetName(),
		Message: message,
	})
}

func (r *Runner) emitActivityResult(message, status string) {
	if !r.isRemoteTarget() {
		return
	}
	r.emit(output.ActivityResultEvent{
		Target:  r.targetName(),
		Message: message,
		Status:  status,
	})
}

func (r *Runner) isRemoteTarget() bool {
	return r.target != nil && r.target.Transport() != target.TransportLocal
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
