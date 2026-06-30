package runner

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/template"
)

// emit dispatches a renderer event, guarding against a nil renderer.
func (r *Runner) emit(evt output.Event) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(evt)
}

// apply executes the task graph against the target. It acquires the runtime
// context from the live target, then delegates to the pure applyResolved loop.
func (r *Runner) apply(ctx context.Context, plan *ExecutionPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dag, err := plan.DAG()
	if err != nil {
		return fmt.Errorf("apply: build DAG: %w", err)
	}

	rt, err := r.buildExecutionContext(ctx)
	if err != nil {
		return err
	}

	return r.applyResolved(ctx, dag, rt)
}

// applyResolved is the pure orchestration loop. It receives an already-built
// RuntimeContext and sequences tasks through the DAG, handling tag filtering,
// dependency checks, when-condition evaluation, binding, execution, and state
// accumulation. The loop is self-contained so tests can inject a fake target
// and a synthetic RuntimeContext without a live connection.
func (r *Runner) applyResolved(ctx context.Context, dag *DAG, rt *template.RuntimeContext) error {
	ordered := dag.TopologicalOrder()
	acc := newApplyAccumulator()

	for _, pt := range ordered {
		if err := r.executeTask(ctx, pt, rt, dag, acc); err != nil {
			if err == errHalt {
				return r.finalizeApply(acc)
			}
			return err
		}
	}
	return r.finalizeApply(acc)
}

// errHalt is a sentinel signalling a non-ignored task failure — the loop
// should break but finalizeApply must still save state.
var errHalt = fmt.Errorf("halt apply loop")

// applyAccumulator tracks per-task outcome counts and the state map during
// apply. It is distinct from the TUI's RunProjection.
type applyAccumulator struct {
	okCount      int
	changedCount int
	failedCount  int
	skippedCount int
	state        *State
	failed       map[string]bool
}

func newApplyAccumulator() *applyAccumulator {
	return &applyAccumulator{
		state:  &State{Tasks: make(map[string]TaskSnapshot)},
		failed: make(map[string]bool),
	}
}

func (acc *applyAccumulator) recordTask(pt *PlanTask, taskName string, sourceParams, params map[string]any, sourceBecome, become map[string]any, status target.Status, message string, dag *DAG) {
	acc.state.RecordTask(newTaskSnapshot(pt, taskName, sourceParams, params, sourceBecome, become, status, message, dag))
	switch status {
	case target.StatusOK:
		acc.okCount++
	case target.StatusChanged:
		acc.changedCount++
	case target.StatusFailed:
		acc.failedCount++
	case target.StatusSkipped:
		acc.skippedCount++
	}
}

func (r *Runner) finalizeApply(acc *applyAccumulator) error {
	acc.state.LastApplied = time.Now()
	if !r.config.DryRun && r.config.StatePath != "" {
		if err := acc.state.Save(r.config.StatePath); err != nil {
			return fmt.Errorf("apply: save state: %w", err)
		}
	}
	if acc.failedCount > 0 {
		return fmt.Errorf("apply: %d task(s) failed", acc.failedCount)
	}
	return nil
}

// executeTask handles one task's full lifecycle: tag filtering, dependency
// gating, when-condition evaluation, template binding + secret resolution,
// target execution, state recording, and event emission.
//
// Returns:
//   - nil: task completed (or was legitimately skipped); continue the loop.
//   - errHalt: non-ignored task failure; state was recorded, caller should
//     finalize (save state) and return.
//   - any other error: unrecoverable — binding, when, or context failure;
//     caller should return immediately without saving state.
func (r *Runner) executeTask(ctx context.Context, pt *PlanTask, rt *template.RuntimeContext, dag *DAG, acc *applyAccumulator) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Tag filtering.
	if !r.taskMatchesTags(pt) {
		r.emit(output.TaskSkippedEvent{
			Target:   r.targetName(),
			TaskID:   pt.ID,
			TaskName: pt.Name,
			Reason:   "tag-filtered",
		})
		acc.recordTask(pt, pt.Name, pt.Params, pt.Params, pt.Become, pt.Become, target.StatusSkipped, "tag-filtered", nil)
		return nil
	}

	// Dependency check: skip if any dependency failed (unless ignore_errors).
	depFailed := false
	depIDs, err := dag.DependencyIDs(pt)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	for _, depID := range depIDs {
		if acc.failed[depID] {
			depFailed = true
			break
		}
	}
	if depFailed && !pt.IgnoreErrors {
		r.emit(output.TaskSkippedEvent{
			Target:   r.targetName(),
			TaskID:   pt.ID,
			TaskName: pt.Name,
			Reason:   "dependency-failed",
		})
		acc.recordTask(pt, pt.Name, pt.Params, pt.Params, pt.Become, pt.Become, target.StatusSkipped, "dependency-failed", nil)
		return nil
	}

	// Evaluate when before rendering params/become so skipped tasks do not fail
	// on unrelated template expansion errors.
	whenOK, err := evaluateTaskWhen(pt, rt)
	if err != nil {
		return fmt.Errorf("apply: task %q: evaluate when condition: %w", pt.Name, err)
	}
	if !whenOK {
		r.emit(output.TaskSkippedEvent{
			Target:   r.targetName(),
			TaskID:   pt.ID,
			TaskName: pt.Name,
			Reason:   "when-condition-false",
		})
		acc.recordTask(pt, pt.Name, pt.Params, pt.Params, pt.Become, pt.Become, target.StatusSkipped, "when-condition-false", nil)
		return nil
	}

	// Bind + resolve secrets + execution options in one consolidated step.
	bound, err := bindAndResolveTask(ctx, pt, rt, r.config.Secrets)
	if err != nil {
		return fmt.Errorf("task %q: %w", pt.Name, err)
	}

	// Execute the task against the target.
	slog.Debug("executing task", "task", pt.Name, "module", pt.Module, "id", pt.ID)
	r.emit(output.TaskStartedEvent{
		Target:     r.targetName(),
		TaskID:     pt.ID,
		TaskName:   pt.Name,
		Module:     pt.Module,
		ActionPath: pt.ActionPath,
	})
	taskStartTime := time.Now()
	var onOutput target.OutputFunc
	if r.config.Renderer != nil {
		onOutput = func(line string) {
			r.config.Renderer.Emit(output.TaskOutputEvent{
				TaskID:   pt.ID,
				TaskName: bound.Name,
				Target:   r.targetName(),
				Lines:    []string{line},
			})
		}
	}
	result, execErr := r.target.Execute(ctx, pt.ID, pt.Module, bound.Params, bound.ExecOpts, r.config.DryRun, onOutput)
	elapsedMs := time.Since(taskStartTime).Milliseconds()
	if execErr != nil {
		if !pt.IgnoreErrors {
			r.emit(output.TaskFailedEvent{
				Target:      r.targetName(),
				TaskID:      pt.ID,
				TaskName:    pt.Name,
				ElapsedMs:   elapsedMs,
				ExitCode:    0,
				Output:      result.Output,
				FailMessage: execErr.Error(),
			})
			r.emit(output.DiagnosticEvent{
				Target:  r.targetName(),
				TaskID:  pt.ID,
				Summary: execErr.Error(),
				Source:  pt.Module,
			})
			acc.recordTask(pt, bound.Name, bound.SourceParams, bound.Params, bound.SourceBecome, bound.Become, target.StatusFailed, execErr.Error(), dag)
			acc.failed[pt.ID] = true
			return errHalt
		}
		result = target.Result{Status: target.StatusOK, Message: execErr.Error(), Output: result.Output}
	}

	acc.recordTask(pt, bound.Name, bound.SourceParams, bound.Params, bound.SourceBecome, bound.Become, result.Status, result.Message, dag)

	switch result.Status {
	case target.StatusOK:
		r.emit(output.TaskOKEvent{
			Target:    r.targetName(),
			TaskID:    pt.ID,
			TaskName:  pt.Name,
			ElapsedMs: elapsedMs,
		})
	case target.StatusChanged:
		r.emit(output.TaskChangedEvent{
			Target:    r.targetName(),
			TaskID:    pt.ID,
			TaskName:  pt.Name,
			ElapsedMs: elapsedMs,
		})
	case target.StatusFailed:
		r.emit(output.TaskFailedEvent{
			Target:      r.targetName(),
			TaskID:      pt.ID,
			TaskName:    pt.Name,
			ElapsedMs:   elapsedMs,
			ExitCode:    0,
			Output:      result.Output,
			FailMessage: result.Message,
		})
		r.emit(output.DiagnosticEvent{
			Target:  r.targetName(),
			TaskID:  pt.ID,
			Summary: result.Message,
			Source:  pt.Module,
		})
	case target.StatusSkipped:
		r.emit(output.TaskSkippedEvent{
			Target:   r.targetName(),
			TaskID:   pt.ID,
			TaskName: pt.Name,
			Reason:   result.Message,
		})
	}

	if result.Status == target.StatusFailed && !pt.IgnoreErrors {
		acc.failed[pt.ID] = true
		return errHalt
	}

	return nil
}

// taskMatchesTags returns true if the task should run given the tag config.
func (r *Runner) taskMatchesTags(pt *PlanTask) bool {
	// If --tags specified, task must have at least one matching tag.
	if len(r.config.Tags) > 0 {
		matched := false
		for _, wantTag := range r.config.Tags {
			if slices.Contains(pt.Tags, wantTag) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// If --skip-tags specified, task must have none of the skip tags.
	for _, skipTag := range r.config.SkipTags {
		if slices.Contains(pt.Tags, skipTag) {
			return false
		}
	}

	return true
}

func newTaskSnapshot(pt *PlanTask, taskName string, sourceParams, params map[string]any, sourceBecome, become map[string]any, status target.Status, message string, dag *DAG) TaskSnapshot {
	dependsOn := []string(nil)
	if dag != nil {
		dependsOn, _ = dag.DependencyIDs(pt)
	}

	paramHash := StateParamHash(sourceParams, params, sourceBecome, become)
	summary := StateParamSummary(sourceParams, params, sourceBecome, become)

	return TaskSnapshot{
		TaskKey:      pt.ID,
		TaskName:     taskName,
		Module:       pt.Module,
		DependsOn:    dependsOn,
		ParamHash:    paramHash,
		ParamSummary: summary,
		TaskHash: hashValue(map[string]any{
			"task_key":   pt.ID,
			"task_name":  taskName,
			"module":     pt.Module,
			"depends_on": dependsOn,
			"param_hash": paramHash,
		}),
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// buildExecutionContext connects to the live target, gathers facts, and
// returns a RuntimeContext for template binding. This is the only apply step
// that requires a real target connection.
func (r *Runner) buildExecutionContext(ctx context.Context) (*template.RuntimeContext, error) {
	targetVars := cloneMap(r.config.TargetVars)
	r.emitActivityStart("connecting")
	info, err := r.target.Info(ctx)
	if err != nil {
		r.emitActivityResult("connecting", "failed")
		return nil, fmt.Errorf("apply: target info: %w", err)
	}

	if targetVars == nil {
		targetVars = make(map[string]any)
	}
	if _, ok := targetVars["hostname"]; !ok && info.Hostname != "" {
		targetVars["hostname"] = info.Hostname
	}
	if _, ok := targetVars["name"]; !ok && info.Hostname != "" {
		targetVars["name"] = info.Hostname
	}

	gatherer := facts.New(r.target)
	collected, err := gatherer.Gather(ctx)
	if err != nil {
		r.emitActivityResult("connecting", "failed")
		return nil, fmt.Errorf("apply: gather facts: %w", err)
	}
	r.emitActivityResult("connecting", "ok")

	return &template.RuntimeContext{
		Target: targetVars,
		Facts:  collected.AsMap(),
		Env:    collected.Env,
	}, nil
}

// PreviewTask renders a single PlanTask against the given target vars using
// BindPartial, preserving unknown references. Used for plan previews and
// staged-bundle analysis.
func PreviewTask(task *PlanTask, targetVars map[string]any) (*PlanTask, error) {
	preview := *task
	preview.Params = cloneMap(task.Params)
	preview.Become = cloneMap(task.Become)
	rt := &template.RuntimeContext{Target: targetVars}
	bound, err := bindTask(&preview, rt, template.BindPartial)
	if err != nil {
		return nil, err
	}
	preview.Name = bound.Name
	preview.When = bound.When
	preview.Params = bound.Params
	preview.Become = bound.Become
	return &preview, nil
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	maps.Copy(dst, src)
	return dst
}

func (r *Runner) targetName() string {
	if r.config.TargetName != "" {
		return r.config.TargetName
	}
	if name, ok := r.config.TargetVars["name"].(string); ok && name != "" {
		return name
	}
	if hostname, ok := r.config.TargetVars["hostname"].(string); ok && hostname != "" {
		return hostname
	}
	// Return empty string rather than a hardcoded "localhost" to avoid
	// silently claiming a local identity when no target name is configured.
	return ""
}
