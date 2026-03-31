package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/claytercek/preflight/internal/action"
	"github.com/claytercek/preflight/internal/output"
	"github.com/claytercek/preflight/internal/target"
	"github.com/claytercek/preflight/internal/template"
)

// ExecutionPlan is the result of the Plan phase: a flat, ordered list of tasks
// with all variables resolved.
type ExecutionPlan struct {
	PlaybookName string
	Tasks        []*PlanTask
	Vars         map[string]interface{}
}

// PlanTask is a single task entry in the execution plan.
type PlanTask struct {
	ID           string // unique ID, e.g. "task-0", "task-1"
	Name         string
	Module       string
	Params       map[string]interface{}
	DependsOn    []string
	When         string
	Tags         []string
	IgnoreErrors bool
}

// Plan resolves all action refs, expands tasks into a flat list, resolves
// variables. Returns an ExecutionPlan. Pure computation — no I/O against targets.
func (r *Runner) Plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
	// Merge variables: playbook vars first, then CLI --var flags (CLI wins).
	vars := make(map[string]interface{})
	for k, v := range playbook.Vars {
		vars[k] = v
	}
	for k, v := range r.config.Vars {
		vars[k] = v
	}

	eng := template.New(vars)

	var planTasks []*PlanTask
	idx := 0

	// Expand inline tasks from the playbook.
	for i := range playbook.Tasks {
		task := &playbook.Tasks[i]
		if err := task.ResolveModule(); err != nil {
			return nil, fmt.Errorf("plan: task %d: %w", i, err)
		}

		// If this task uses an action ref (uses:), resolve and inline it.
		if task.Uses != "" {
			resolved, err := r.resolver.Resolve(ctx, task.Uses)
			if err != nil {
				return nil, fmt.Errorf("plan: resolve action %q: %w", task.Uses, err)
			}

			// Inline the action's tasks.
			for j := range resolved.Tasks {
				at := &resolved.Tasks[j]
				if err := at.ResolveModule(); err != nil {
					return nil, fmt.Errorf("plan: action %q task %d: %w", task.Uses, j, err)
				}
				pt, err := buildPlanTask(at, idx, eng)
				if err != nil {
					return nil, err
				}
				planTasks = append(planTasks, pt)
				idx++
			}
			continue
		}

		pt, err := buildPlanTask(task, idx, eng)
		if err != nil {
			return nil, err
		}
		planTasks = append(planTasks, pt)
		idx++
	}

	// Validate the DAG (detects cycles and unknown depends_on refs).
	if _, err := BuildDAG(planTasks); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	return &ExecutionPlan{
		PlaybookName: playbook.Name,
		Tasks:        planTasks,
		Vars:         vars,
	}, nil
}

// buildPlanTask converts an action.Task to a PlanTask, rendering string params.
func buildPlanTask(t *action.Task, idx int, eng *template.Engine) (*PlanTask, error) {
	id := fmt.Sprintf("task-%d", idx)

	// Render params through the template engine.
	renderedParams, err := eng.RenderMap(t.Params)
	if err != nil {
		return nil, fmt.Errorf("plan: task %q params: %w", t.Name, err)
	}

	return &PlanTask{
		ID:           id,
		Name:         t.Name,
		Module:       t.Module,
		Params:       renderedParams,
		DependsOn:    t.DependsOn,
		When:         t.When,
		Tags:         t.Tags,
		IgnoreErrors: t.IgnoreErrors,
	}, nil
}

// Fetch downloads remote action refs not yet in cache.
// Stub for now — validates that the plan is non-nil.
func (r *Runner) Fetch(_ context.Context, plan *ExecutionPlan) error {
	if plan == nil {
		return fmt.Errorf("fetch: nil execution plan")
	}
	// TODO: download any remote action refs referenced in the plan.
	return nil
}

// Stage assembles a self-contained artifact bundle (zip).
// Stub for now.
func (r *Runner) Stage(_ context.Context, plan *ExecutionPlan) error {
	if plan == nil {
		return fmt.Errorf("stage: nil execution plan")
	}
	// TODO: assemble artifact bundle for air-gapped targets.
	return nil
}

// Apply executes the task graph against the target.
func (r *Runner) Apply(ctx context.Context, plan *ExecutionPlan) error {
	dag, err := BuildDAG(plan.Tasks)
	if err != nil {
		return fmt.Errorf("apply: build DAG: %w", err)
	}

	ordered := dag.TopologicalOrder()
	eng := template.New(plan.Vars)

	state := &State{
		Results: make(map[string]TaskResult),
	}

	// Track outcome counts for the play recap.
	var okCount, changedCount, failedCount, skippedCount int

	// Track which tasks have succeeded for dependency checking.
	succeeded := make(map[string]bool)
	failed := make(map[string]bool)

	for _, pt := range ordered {
		// Tag filtering.
		if !r.taskMatchesTags(pt) {
			r.emitTaskResult(pt, target.StatusSkipped, "tag-filtered")
			state.Record(TaskResult{
				TaskID:   pt.ID,
				TaskName: pt.Name,
				Status:   target.StatusSkipped,
				Timestamp: time.Now(),
			})
			skippedCount++
			succeeded[pt.ID] = false
			continue
		}

		// Dependency check: skip if any dependency failed (unless ignore_errors).
		depFailed := false
		for _, dep := range pt.DependsOn {
			depID := dag.nameToID[dep]
			if failed[depID] {
				depFailed = true
				break
			}
		}
		if depFailed && !pt.IgnoreErrors {
			r.emitTaskResult(pt, target.StatusSkipped, "dependency-failed")
			state.Record(TaskResult{
				TaskID:   pt.ID,
				TaskName: pt.Name,
				Status:   target.StatusSkipped,
				Timestamp: time.Now(),
			})
			skippedCount++
			succeeded[pt.ID] = false
			continue
		}

		// Evaluate when: condition.
		if pt.When != "" {
			ok, err := eng.RenderBool(pt.When)
			if err != nil {
				return fmt.Errorf("apply: task %q: evaluate when condition: %w", pt.Name, err)
			}
			if !ok {
				r.emitTaskResult(pt, target.StatusSkipped, "when-condition-false")
				state.Record(TaskResult{
					TaskID:   pt.ID,
					TaskName: pt.Name,
					Status:   target.StatusSkipped,
					Timestamp: time.Now(),
				})
				skippedCount++
				succeeded[pt.ID] = false
				continue
			}
		}

		// Execute the task against the target.
		result, execErr := r.target.Execute(ctx, pt.ID, pt.Module, pt.Params, r.config.DryRun)
		if execErr != nil {
			if !pt.IgnoreErrors {
				r.emitTaskResult(pt, target.StatusFailed, execErr.Error())
				state.Record(TaskResult{
					TaskID:   pt.ID,
					TaskName: pt.Name,
					Status:   target.StatusFailed,
					Timestamp: time.Now(),
				})
				failedCount++
				failed[pt.ID] = true
				continue
			}
			// IgnoreErrors: treat as ok.
			result = target.Result{
				TaskID:  pt.ID,
				Status:  target.StatusFailed,
				Message: execErr.Error(),
			}
		}

		state.Record(TaskResult{
			TaskID:    pt.ID,
			TaskName:  pt.Name,
			Status:    result.Status,
			Timestamp: time.Now(),
		})

		r.emitTaskResult(pt, result.Status, result.Message)

		switch result.Status {
		case target.StatusOK:
			okCount++
			succeeded[pt.ID] = true
		case target.StatusChanged:
			changedCount++
			succeeded[pt.ID] = true
		case target.StatusFailed:
			failedCount++
			if !pt.IgnoreErrors {
				failed[pt.ID] = true
			} else {
				succeeded[pt.ID] = true
			}
		case target.StatusSkipped:
			skippedCount++
			succeeded[pt.ID] = false
		}
	}

	state.LastApplied = time.Now()

	// Emit play recap.
	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.Event{
			Type:         output.EventPlayEnd,
			PlayName:     plan.PlaybookName,
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

// taskMatchesTags returns true if the task should run given the tag config.
func (r *Runner) taskMatchesTags(pt *PlanTask) bool {
	// If --tags specified, task must have at least one matching tag.
	if len(r.config.Tags) > 0 {
		matched := false
		for _, wantTag := range r.config.Tags {
			for _, taskTag := range pt.Tags {
				if wantTag == taskTag {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}

	// If --skip-tags specified, task must have none of the skip tags.
	for _, skipTag := range r.config.SkipTags {
		for _, taskTag := range pt.Tags {
			if skipTag == taskTag {
				return false
			}
		}
	}

	return true
}

// emitTaskResult emits a task_result event to the renderer.
func (r *Runner) emitTaskResult(pt *PlanTask, status target.Status, message string) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:     output.EventTaskResult,
		TaskName: pt.Name,
		Status:   string(status),
		Message:  message,
	})
}
