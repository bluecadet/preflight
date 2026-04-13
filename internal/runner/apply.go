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

// apply executes the task graph against the target.
func (r *Runner) apply(ctx context.Context, plan *ExecutionPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dag, err := BuildDAG(plan.Tasks)
	if err != nil {
		return fmt.Errorf("apply: build DAG: %w", err)
	}

	ordered := dag.TopologicalOrder()
	execCtx, err := r.buildExecutionContext(ctx)
	if err != nil {
		return err
	}

	state := &State{
		Tasks: make(map[string]TaskSnapshot),
	}

	// Track outcome counts for the play recap.
	var okCount, changedCount, failedCount, skippedCount int

	failed := make(map[string]bool)

	finishApply := func() error {
		state.LastApplied = time.Now()
		if !r.config.DryRun && r.config.StatePath != "" {
			if err := state.Save(r.config.StatePath); err != nil {
				return fmt.Errorf("apply: save state: %w", err)
			}
		}

		if r.config.Renderer != nil {
			r.config.Renderer.Emit(output.PlayEndEvent{
				Target:       r.targetName(),
				OKCount:      okCount,
				ChangedCount: changedCount,
				FailedCount:  failedCount,
				SkippedCount: skippedCount,
			})
		}

		if failedCount > 0 {
			return fmt.Errorf("apply: %d task(s) failed", failedCount)
		}
		return nil
	}

	for _, pt := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Tag filtering.
		if !r.taskMatchesTags(pt) {
			r.emitTaskResult(pt, target.StatusSkipped, "tag-filtered", nil)
			state.RecordTask(newTaskSnapshot(pt, pt.Name, pt.Params, pt.Params, pt.Become, pt.Become, target.StatusSkipped, "tag-filtered", nil))
			skippedCount++
			continue
		}

		// Dependency check: skip if any dependency failed (unless ignore_errors).
		depFailed := false
		for _, dep := range pt.DependsOn {
			depID, ok := dag.nameToID[dep]
			if !ok {
				return fmt.Errorf("apply: task %q: dependency %q not found in DAG", pt.Name, dep)
			}
			if failed[depID] {
				depFailed = true
				break
			}
		}
		if depFailed && !pt.IgnoreErrors {
			r.emitTaskResult(pt, target.StatusSkipped, "dependency-failed", nil)
			state.RecordTask(newTaskSnapshot(pt, pt.Name, pt.Params, pt.Params, pt.Become, pt.Become, target.StatusSkipped, "dependency-failed", nil))
			skippedCount++
			continue
		}

		// Evaluate when: condition.
		if pt.When != "" {
			ok, err := renderTaskWhen(pt, execCtx)
			if err != nil {
				return fmt.Errorf("apply: task %q: evaluate when condition: %w", pt.Name, err)
			}
			if !ok {
				r.emitTaskResult(pt, target.StatusSkipped, "when-condition-false", nil)
				state.RecordTask(newTaskSnapshot(pt, pt.Name, pt.Params, pt.Params, pt.Become, pt.Become, target.StatusSkipped, "when-condition-false", nil))
				skippedCount++
				continue
			}
		}

		params, taskName, err := renderTaskParams(pt, execCtx)
		if err != nil {
			return fmt.Errorf("task %q: %w", pt.Name, err)
		}
		sourceBecome, execOpts, err := renderTaskExecutionOptions(pt, execCtx)
		if err != nil {
			return fmt.Errorf("task %q: %w", pt.Name, err)
		}
		stateSource := params
		resolvedBecome := sourceBecome
		if r.config.Secrets != nil && r.config.Secrets.HasProviders() {
			params, err = r.config.Secrets.ResolveMap(ctx, params)
			if err != nil {
				return fmt.Errorf("apply: task %q: %w", pt.Name, err)
			}
			resolvedBecome, execOpts, err = resolveExecutionOptions(ctx, r.config.Secrets, sourceBecome)
			if err != nil {
				return fmt.Errorf("apply: task %q: %w", pt.Name, err)
			}
		}

		// Execute the task against the target.
		slog.Debug("executing task", "task", pt.Name, "module", pt.Module, "id", pt.ID)
		r.emitTaskStart(pt)
		var onOutput target.OutputFunc
		if r.config.Renderer != nil {
			onOutput = func(line string) {
				r.config.Renderer.Emit(output.TaskOutputEvent{
					TaskID:   pt.ID,
					TaskName: taskName,
					Target:   r.targetName(),
					Lines:    []string{line},
				})
			}
		}
		result, execErr := r.target.Execute(ctx, pt.ID, pt.Module, params, execOpts, r.config.DryRun, onOutput)
		if execErr != nil {
			if !pt.IgnoreErrors {
				r.emitTaskResult(pt, target.StatusFailed, execErr.Error(), result.Output)
				state.RecordTask(newTaskSnapshot(pt, taskName, stateSource, params, sourceBecome, resolvedBecome, target.StatusFailed, execErr.Error(), dag))
				failedCount++
				failed[pt.ID] = true
				return finishApply()
			}
			// IgnoreErrors: treat as ok.
			result = target.Result{
				Status:  target.StatusOK,
				Message: execErr.Error(),
				Output:  result.Output,
			}
		}

		state.RecordTask(newTaskSnapshot(pt, taskName, stateSource, params, sourceBecome, resolvedBecome, result.Status, result.Message, dag))

		r.emitTaskResult(pt, result.Status, result.Message, result.Output)

		switch result.Status {
		case target.StatusOK:
			okCount++
		case target.StatusChanged:
			changedCount++
		case target.StatusFailed:
			failedCount++
			if !pt.IgnoreErrors {
				failed[pt.ID] = true
				return finishApply()
			}
		case target.StatusSkipped:
			skippedCount++
		}
	}

	return finishApply()
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

// emitTaskResult emits a task_result event to the renderer.
func (r *Runner) emitTaskResult(pt *PlanTask, status target.Status, message string, taskOutput []string) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.TaskResultEvent{
		TaskID:     pt.ID,
		TaskName:   pt.Name,
		ActionPath: pt.ActionPath,
		Target:     r.targetName(),
		Status:     string(status),
		Message:    message,
		Output:     taskOutput,
	})
}

func newTaskSnapshot(pt *PlanTask, taskName string, sourceParams, params map[string]any, sourceBecome, become map[string]any, status target.Status, message string, dag *DAG) TaskSnapshot {
	dependsOn := make([]string, 0, len(pt.DependsOn))
	if dag != nil {
		for _, depName := range pt.DependsOn {
			if depID, ok := dag.nameToID[depName]; ok {
				dependsOn = append(dependsOn, depID)
			}
		}
	}
	slices.Sort(dependsOn)

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

type executionContext struct {
	target map[string]any
	facts  map[string]any
	env    map[string]string
}

// taskEngine builds a template engine pre-loaded with the task's variables and
// the execution context (target info, facts, environment). Used by all task
// rendering functions to avoid repeating the same construction chain.
func taskEngine(task *PlanTask, execCtx *executionContext) *template.Engine {
	return template.New(task.TemplateVars).
		WithTarget(execCtx.target).
		WithFacts(execCtx.facts).
		WithEnv(execCtx.env)
}

func (r *Runner) buildExecutionContext(ctx context.Context) (*executionContext, error) {
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

	return &executionContext{
		target: targetVars,
		facts:  collected.AsMap(),
		env:    collected.Env,
	}, nil
}

func renderTaskWhen(task *PlanTask, execCtx *executionContext) (bool, error) {
	if task.When == "" {
		return true, nil
	}
	return taskEngine(task, execCtx).RenderBool(task.When)
}

func renderTaskParams(task *PlanTask, execCtx *executionContext) (map[string]any, string, error) {
	eng := taskEngine(task, execCtx)

	params, err := eng.RenderMap(task.Params)
	if err != nil {
		return nil, "", err
	}

	name := task.Name
	if task.Name != "" {
		name, err = eng.Render(task.Name)
		if err != nil {
			return nil, "", err
		}
	}

	return params, name, nil
}

func renderTaskExecutionOptions(task *PlanTask, execCtx *executionContext) (map[string]any, target.ExecutionOptions, error) {
	if len(task.Become) == 0 {
		return nil, target.ExecutionOptions{}, nil
	}

	become, err := taskEngine(task, execCtx).RenderMap(task.Become)
	if err != nil {
		return nil, target.ExecutionOptions{}, err
	}
	opts, err := target.NormalizeExecutionOptions(map[string]any{"become": become})
	if err != nil {
		return nil, target.ExecutionOptions{}, err
	}
	return become, opts, nil
}

func PreviewTask(task *PlanTask, targetVars map[string]any) (*PlanTask, error) {
	preview := *task
	preview.TemplateVars = cloneMap(task.TemplateVars)
	preview.Params = cloneMap(task.Params)
	preview.Become = cloneMap(task.Become)

	eng := template.New(task.TemplateVars).WithTarget(targetVars).WithPreserveUnknown()

	if preview.Name != "" {
		name, err := eng.Render(preview.Name)
		if err != nil {
			return nil, err
		}
		preview.Name = name
	}

	if preview.When != "" {
		when, err := eng.Render(preview.When)
		if err != nil {
			return nil, err
		}
		preview.When = when
	}

	params, err := eng.RenderMap(task.Params)
	if err != nil {
		return nil, err
	}
	preview.Params = params

	if len(task.Become) > 0 {
		become, err := eng.RenderMap(task.Become)
		if err != nil {
			return nil, err
		}
		preview.Become = become
	}

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

func canonicalizeBecome(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := cloneMap(src)
	if _, ok := dst["enabled"]; !ok {
		dst["enabled"] = true
	}
	return dst
}

func mergeBecome(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	if enabled, ok := override["enabled"].(bool); ok && !enabled {
		return canonicalizeBecome(override)
	}

	dst := cloneMap(base)
	if dst == nil {
		dst = make(map[string]any)
	}
	if len(override) > 0 {
		maps.Copy(dst, canonicalizeBecome(override))
	}
	if len(dst) == 0 {
		return nil
	}
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
