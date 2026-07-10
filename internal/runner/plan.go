package runner

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/template"
)

// ExecutionPlan is the result of the Plan phase: a flat, ordered list of tasks
// with all variables resolved.
type ExecutionPlan struct {
	PlaybookName string
	Tasks        []*PlanTask
	dag          *DAG
}

// PlanTask is a single task entry in the execution plan.
type PlanTask struct {
	ID           string // unique ID, e.g. "task-0", "task-1"
	Name         string
	Ref          string
	ActionPath   string // human-readable parent path, e.g. "Apply machine baseline/Configure computer name"
	Module       string
	Params       map[string]any
	Become       map[string]any
	Scope        *template.Scope
	DependsOn    []string
	When         string
	Tags         []string
	IgnoreErrors bool
}

// plan resolves all action refs, expands tasks into a flat list, resolves
// variables. Returns an ExecutionPlan. Pure computation — no I/O against targets.
func (r *Runner) plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	preflightVar := map[string]any{
		"project":     r.config.ProjectName,
		"environment": r.config.ProjectEnv,
	}
	rootScope := template.NewScope(
		r.config.ProjectVars,
		r.config.InventoryVars,
		map[string]any{"preflight": preflightVar},
		playbook.Vars,
		r.config.Vars,
	)

	var planTasks []*PlanTask
	counter := newLineageCounter()

	for i := range playbook.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := &playbook.Tasks[i]
		if err := r.expandTask(ctx, task, rootScope, &planTasks, counter, canonicalizeBecome(playbook.Defaults.Become), nil, nil, fmt.Sprintf("task %d", i)); err != nil {
			return nil, fmt.Errorf("plan: %w", err)
		}
	}

	// Validate and keep the DAG so apply/state use the same dependency
	// resolution that planning accepted.
	dag, err := BuildDAG(planTasks)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	if err := r.validatePlanTasks(planTasks); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	return &ExecutionPlan{
		PlaybookName: playbook.Name,
		Tasks:        planTasks,
		dag:          dag,
	}, nil
}

type lineageCounter struct {
	counts map[string]int
}

func newLineageCounter() *lineageCounter {
	return &lineageCounter{counts: make(map[string]int)}
}

func (s *lineageCounter) next(base string) string {
	s.counts[base]++
	count := s.counts[base]
	if count == 1 {
		return base
	}
	return base + "-" + strconv.Itoa(count)
}

func (r *Runner) expandTask(ctx context.Context, task *action.Task, taskScope *template.Scope, planTasks *[]*PlanTask, counter *lineageCounter, inheritedBecome map[string]any, lineage []string, displayLineage []string, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := task.ResolveModule(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	segment := counter.next(taskLineageSegment(task))
	currentLineage := append(append([]string{}, lineage...), segment)
	displaySegment := taskDisplaySegment(task)
	currentDisplayLineage := append(append([]string{}, displayLineage...), displaySegment)
	taskBecome := mergeBecome(inheritedBecome, task.Become)

	if task.Uses == "" {
		pt, err := buildPlanTask(task, currentLineage, currentDisplayLineage, taskBecome, taskScope)
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

	childScope, err := deriveActionScope(task, resolved, taskScope)
	if err != nil {
		return fmt.Errorf("prepare action %q inputs: %w", task.Uses, err)
	}

	childBecome := mergeBecome(inheritedBecome, resolved.Defaults.Become)
	childBecome = mergeBecome(childBecome, task.Become)
	childCounter := newLineageCounter()
	for j := range resolved.Tasks {
		at := &resolved.Tasks[j]
		childLabel := fmt.Sprintf("action %q task %d", task.Uses, j)
		if err := r.expandTask(ctx, at, childScope, planTasks, childCounter, childBecome, currentLineage, currentDisplayLineage, childLabel); err != nil {
			return err
		}
	}
	return nil
}

// deriveActionScope builds a derived scope for an action's child tasks.
// The parent scope's vars are shallow-copied, action input defaults are
// applied, and the task's With values (rendered against the parent scope) are
// overlaid on top. Required input validation is preserved.
func deriveActionScope(task *action.Task, resolved *action.Action, parent *template.Scope) (*template.Scope, error) {
	overlay := make(map[string]any)
	for name, input := range resolved.Inputs {
		if input.Default != nil {
			overlay[name] = input.Default
		}
	}
	eng := parent.Engine(nil, template.BindPartial)
	renderedWith, err := eng.RenderMap(task.With)
	if err != nil {
		return nil, err
	}
	maps.Copy(overlay, renderedWith)

	childScope := template.NewDerivedScope(parent, overlay)
	for name, input := range resolved.Inputs {
		if !input.Required {
			continue
		}
		if value, ok := childScope.Vars[name]; !ok || value == nil || value == "" {
			return nil, fmt.Errorf("required input %q is missing", name)
		}
	}
	return childScope, nil
}

// buildPlanTask converts an action.Task to a PlanTask while preserving raw
// templates for later per-target rendering.
func buildPlanTask(t *action.Task, lineage []string, displayLineage []string, become map[string]any, taskScope *template.Scope) (*PlanTask, error) {
	id := strings.Join(lineage, "/")
	ancestorLineage := lineage[:max(len(lineage)-1, 0)]
	// ActionPath is the display lineage minus the leaf (the task's own name).
	var actionPath string
	if len(displayLineage) > 1 {
		actionPath = strings.Join(displayLineage[:len(displayLineage)-1], "/")
	}
	rawParams := cloneMap(t.Params)
	dependsOn := make([]string, 0, len(t.DependsOn))
	for _, dep := range t.DependsOn {
		dependsOn = append(dependsOn, lineageDependencyRef(ancestorLineage, dep))
	}

	return &PlanTask{
		ID:           id,
		Name:         t.Name,
		Ref:          lineageDependencyRef(ancestorLineage, t.Key()),
		ActionPath:   actionPath,
		Module:       t.Module,
		Params:       rawParams,
		Become:       cloneMap(become),
		Scope:        taskScope,
		DependsOn:    dependsOn,
		When:         t.When,
		Tags:         t.Tags,
		IgnoreErrors: t.IgnoreErrors,
	}, nil
}

func lineageDependencyRef(lineage []string, ref string) string {
	if ref == "" || len(lineage) == 0 {
		return ref
	}
	return strings.Join(lineage, "/") + "::" + ref
}

// taskDisplaySegment returns the human-readable label for a task in the display lineage.
func taskDisplaySegment(task *action.Task) string {
	if task.Name != "" {
		return task.Name
	}
	kind := task.Uses
	if kind == "" {
		kind = task.Module
	}
	if kind == "" {
		return "task"
	}
	if i := strings.LastIndex(kind, "/"); i >= 0 {
		kind = kind[i+1:]
	}
	return kind
}

func taskLineageSegment(task *action.Task) string {
	if task.Name != "" {
		return sanitizeLineageSegment(task.Name)
	}
	kind := task.Uses
	if kind == "" {
		kind = task.Module
	}
	if kind == "" {
		return "task"
	}
	// Use only the leaf of the action ref (e.g. "windows-power" from "preflight/windows-power").
	if i := strings.LastIndex(kind, "/"); i >= 0 {
		kind = kind[i+1:]
	}
	return sanitizeLineageSegment(kind)
}

func sanitizeLineageSegment(s string) string {
	return sanitizeSlug(s, "task")
}
