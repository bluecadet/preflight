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
	Vars         map[string]any
}

// PlanTask is a single task entry in the execution plan.
type PlanTask struct {
	ID           string // unique ID, e.g. "task-0", "task-1"
	Name         string
	ActionPath   string // human-readable parent path, e.g. "Apply machine baseline/Configure computer name"
	Module       string
	Params       map[string]any
	Become       map[string]any
	TemplateVars map[string]any
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

	varStore := template.NewVarStore()
	varStore.SetMap(template.LayerProject, r.config.ProjectVars)
	varStore.SetMap(template.LayerInventoryVars, r.config.InventoryVars)
	varStore.Set(template.LayerProject, "preflight", map[string]any{
		"project":     r.config.ProjectName,
		"environment": r.config.ProjectEnv,
	})
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
		if err := r.expandTask(ctx, task, vars, &planTasks, scope, canonicalizeBecome(playbook.Defaults.Become), nil, nil, fmt.Sprintf("task %d", i)); err != nil {
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

func (r *Runner) expandTask(ctx context.Context, task *action.Task, vars map[string]any, planTasks *[]*PlanTask, scope *expansionScope, inheritedBecome map[string]any, lineage []string, displayLineage []string, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := task.ResolveModule(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	segment := scope.next(taskLineageSegment(task))
	currentLineage := append(append([]string{}, lineage...), segment)
	displaySegment := taskDisplaySegment(task)
	currentDisplayLineage := append(append([]string{}, displayLineage...), displaySegment)
	taskBecome := mergeBecome(inheritedBecome, task.Become)

	if task.Uses == "" {
		pt, err := buildPlanTask(task, currentLineage, currentDisplayLineage, taskBecome, vars)
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

	childBecome := mergeBecome(inheritedBecome, resolved.Defaults.Become)
	childBecome = mergeBecome(childBecome, task.Become)
	childScope := newExpansionScope()
	for j := range resolved.Tasks {
		at := &resolved.Tasks[j]
		childLabel := fmt.Sprintf("action %q task %d", task.Uses, j)
		if err := r.expandTask(ctx, at, childVars, planTasks, childScope, childBecome, currentLineage, currentDisplayLineage, childLabel); err != nil {
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
func buildPlanTask(t *action.Task, lineage []string, displayLineage []string, become map[string]any, vars map[string]any) (*PlanTask, error) {
	id := strings.Join(lineage, "/")
	// ActionPath is the display lineage minus the leaf (the task's own name).
	var actionPath string
	if len(displayLineage) > 1 {
		actionPath = strings.Join(displayLineage[:len(displayLineage)-1], "/")
	}
	rawParams := cloneMap(t.Params)
	templateVars := cloneMap(vars)

	return &PlanTask{
		ID:           id,
		Name:         t.Name,
		ActionPath:   actionPath,
		Module:       t.Module,
		Params:       rawParams,
		Become:       cloneMap(become),
		TemplateVars: templateVars,
		DependsOn:    t.DependsOn,
		When:         t.When,
		Tags:         t.Tags,
		IgnoreErrors: t.IgnoreErrors,
	}, nil
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
