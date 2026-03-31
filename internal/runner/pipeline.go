package runner

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/template"
)

// ExecutionPlan is the result of the Plan phase: a flat, ordered list of tasks
// with all variables resolved.
type ExecutionPlan struct {
	PlaybookName string
	Tasks        []*PlanTask
	Vars         map[string]any
}

// PlanTask is a single task entry in the execution plan.
type PlanTask struct {
	ID           string // unique ID, e.g. "task-0", "task-1"
	Name         string
	Module       string
	Params       map[string]any
	TemplateVars map[string]any
	DependsOn    []string
	When         string
	Tags         []string
	IgnoreErrors bool
}

// Plan resolves all action refs, expands tasks into a flat list, resolves
// variables. Returns an ExecutionPlan. Pure computation — no I/O against targets.
func (r *Runner) Plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	varStore := template.NewVarStore()
	varStore.SetMap(template.LayerProject, r.config.ProjectVars)
	varStore.SetMap(template.LayerGroupVars, r.config.InventoryVars)
	varStore.SetMap(template.LayerPlaybook, playbook.Vars)
	varStore.SetMap(template.LayerCLI, r.config.Vars)
	vars := varStore.Merge()

	var planTasks []*PlanTask
	scope := newExpansionScope()

	for i := range playbook.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := &playbook.Tasks[i]
		if err := r.expandTask(ctx, task, vars, &planTasks, scope, nil, fmt.Sprintf("task %d", i)); err != nil {
			return nil, fmt.Errorf("plan: %w", err)
		}
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

type expansionScope struct {
	counts map[string]int
}

func newExpansionScope() *expansionScope {
	return &expansionScope{counts: make(map[string]int)}
}

func (s *expansionScope) next(base string) string {
	s.counts[base]++
	count := s.counts[base]
	if count == 1 {
		return base
	}
	return base + "-" + strconv.Itoa(count)
}

func (r *Runner) expandTask(ctx context.Context, task *action.Task, vars map[string]any, planTasks *[]*PlanTask, scope *expansionScope, lineage []string, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := task.ResolveModule(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	segment := scope.next(taskLineageSegment(task))
	currentLineage := append(append([]string{}, lineage...), segment)

	if task.Uses == "" {
		pt, err := buildPlanTask(task, currentLineage, vars)
		if err != nil {
			return err
		}
		*planTasks = append(*planTasks, pt)
		return nil
	}

	resolved, err := r.resolver.Resolve(ctx, task.Uses)
	if err != nil {
		return fmt.Errorf("resolve action %q: %w", task.Uses, err)
	}

	childVars, err := actionInputVars(task, resolved, vars)
	if err != nil {
		return fmt.Errorf("prepare action %q inputs: %w", task.Uses, err)
	}

	childScope := newExpansionScope()
	for j := range resolved.Tasks {
		at := &resolved.Tasks[j]
		childLabel := fmt.Sprintf("action %q task %d", task.Uses, j)
		if err := r.expandTask(ctx, at, childVars, planTasks, childScope, currentLineage, childLabel); err != nil {
			return err
		}
	}
	return nil
}

func actionInputVars(task *action.Task, resolved *action.Action, parentVars map[string]any) (map[string]any, error) {
	childVars := make(map[string]any)
	maps.Copy(childVars, parentVars)
	for name, input := range resolved.Inputs {
		if input.Default != nil {
			childVars[name] = input.Default
		}
	}
	eng := template.New(parentVars).WithPreserveUnknown()
	renderedWith, err := eng.RenderMap(task.With)
	if err != nil {
		return nil, err
	}
	maps.Copy(childVars, renderedWith)
	for key, value := range renderedWith {
		if before, ok := strings.CutSuffix(key, "_from"); ok {
			childVars[before] = value
		}
	}
	for name, input := range resolved.Inputs {
		if !input.Required {
			continue
		}
		if value, ok := childVars[name]; !ok || value == nil || value == "" {
			return nil, fmt.Errorf("required input %q is missing", name)
		}
	}
	return childVars, nil
}

// buildPlanTask converts an action.Task to a PlanTask while preserving raw
// templates for later per-target rendering.
func buildPlanTask(t *action.Task, lineage []string, vars map[string]any) (*PlanTask, error) {
	id := strings.Join(lineage, "/")
	rawParams := cloneMap(t.Params)
	templateVars := cloneMap(vars)

	return &PlanTask{
		ID:           id,
		Name:         t.Name,
		Module:       t.Module,
		Params:       rawParams,
		TemplateVars: templateVars,
		DependsOn:    t.DependsOn,
		When:         t.When,
		Tags:         t.Tags,
		IgnoreErrors: t.IgnoreErrors,
	}, nil
}

func taskLineageSegment(task *action.Task) string {
	kind := task.Uses
	if kind == "" {
		kind = task.Module
	}
	if kind == "" {
		kind = "task"
	}
	name := task.Name
	if name == "" {
		name = kind
	}
	return sanitizeLineageSegment(kind + "-" + name)
}

func sanitizeLineageSegment(s string) string {
	if s == "" {
		return "task"
	}
	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "task"
	}
	return out
}

// Fetch downloads remote action refs not yet in cache.
func (r *Runner) Fetch(ctx context.Context, playbook *action.Playbook) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if playbook == nil {
		return fmt.Errorf("fetch: nil playbook")
	}

	_, err := action.FetchRefs(ctx, r.resolver, action.PlaybookUses(playbook))
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// Stage assembles a self-contained artifact bundle (zip).
func (r *Runner) Stage(ctx context.Context, plan *ExecutionPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("stage: nil execution plan")
	}
	return fmt.Errorf("stage phase not implemented in local-only mode")
}

// Apply executes the task graph against the target.
func (r *Runner) Apply(ctx context.Context, plan *ExecutionPlan) error {
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

	// Track which tasks have succeeded for dependency checking.
	succeeded := make(map[string]bool)
	failed := make(map[string]bool)

	for _, pt := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Tag filtering.
		if !r.taskMatchesTags(pt) {
			r.emitTaskResult(pt, target.StatusSkipped, "tag-filtered")
			state.RecordTask(newTaskSnapshot(pt, pt.Name, pt.Params, target.StatusSkipped, "tag-filtered", nil))
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
			state.RecordTask(newTaskSnapshot(pt, pt.Name, pt.Params, target.StatusSkipped, "dependency-failed", dag))
			skippedCount++
			succeeded[pt.ID] = false
			continue
		}

		// Evaluate when: condition.
		if pt.When != "" {
			ok, err := renderTaskWhen(pt, execCtx)
			if err != nil {
				return fmt.Errorf("apply: task %q: evaluate when condition: %w", pt.Name, err)
			}
			if !ok {
				r.emitTaskResult(pt, target.StatusSkipped, "when-condition-false")
				state.RecordTask(newTaskSnapshot(pt, pt.Name, pt.Params, target.StatusSkipped, "when-condition-false", dag))
				skippedCount++
				succeeded[pt.ID] = false
				continue
			}
		}

		params, taskName, err := renderTaskParams(pt, execCtx)
		if err != nil {
			return fmt.Errorf("apply: task %q: render params: %w", pt.Name, err)
		}
		if r.config.Secrets != nil && r.config.Secrets.HasProviders() {
			params, err = r.config.Secrets.ResolveMap(ctx, params)
			if err != nil {
				return fmt.Errorf("apply: task %q: %w", pt.Name, err)
			}
		}

		// Execute the task against the target.
		result, execErr := r.target.Execute(ctx, pt.ID, pt.Module, params, r.config.DryRun)
		if execErr != nil {
			if !pt.IgnoreErrors {
				r.emitTaskResult(pt, target.StatusFailed, execErr.Error())
				state.RecordTask(newTaskSnapshot(pt, taskName, params, target.StatusFailed, execErr.Error(), dag))
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

		state.RecordTask(newTaskSnapshot(pt, taskName, params, result.Status, result.Message, dag))

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
	if !r.config.DryRun && r.config.StatePath != "" {
		if err := state.Save(r.config.StatePath); err != nil {
			return fmt.Errorf("apply: save state: %w", err)
		}
	}

	// Emit play recap.
	if r.config.Renderer != nil {
		r.config.Renderer.Emit(output.Event{
			Type:         output.EventPlayEnd,
			PlayName:     plan.PlaybookName,
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
func (r *Runner) emitTaskResult(pt *PlanTask, status target.Status, message string) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:     output.EventTaskResult,
		TaskName: pt.Name,
		Target:   r.targetName(),
		Status:   string(status),
		Message:  message,
	})
}

func newTaskSnapshot(pt *PlanTask, taskName string, params map[string]any, status target.Status, message string, dag *DAG) TaskSnapshot {
	dependsOn := make([]string, 0, len(pt.DependsOn))
	if dag != nil {
		for _, depName := range pt.DependsOn {
			if depID, ok := dag.nameToID[depName]; ok {
				dependsOn = append(dependsOn, depID)
			}
		}
	}
	slices.Sort(dependsOn)

	paramHash := ParamHash(params)
	summary := SummarizeParams(params)

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

func (r *Runner) buildExecutionContext(ctx context.Context) (*executionContext, error) {
	targetVars := cloneMap(r.config.TargetVars)
	info, err := r.target.Info(ctx)
	if err != nil {
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
		return nil, fmt.Errorf("apply: gather facts: %w", err)
	}

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

	eng := template.New(task.TemplateVars).
		WithTarget(execCtx.target).
		WithFacts(execCtx.facts).
		WithEnv(execCtx.env)
	return eng.RenderBool(task.When)
}

func renderTaskParams(task *PlanTask, execCtx *executionContext) (map[string]any, string, error) {
	eng := template.New(task.TemplateVars).
		WithTarget(execCtx.target).
		WithFacts(execCtx.facts).
		WithEnv(execCtx.env)

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

func PreviewTask(task *PlanTask, targetVars map[string]any) (*PlanTask, error) {
	preview := *task
	preview.TemplateVars = cloneMap(task.TemplateVars)
	preview.Params = cloneMap(task.Params)

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
	return "localhost"
}
