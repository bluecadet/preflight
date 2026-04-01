package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/bundle"
	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/tasklog"
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
	TaskPath     string // display path like "2.1"
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
		if err := r.expandTask(ctx, task, vars, &planTasks, scope, nil, []int{i + 1}, fmt.Sprintf("task %d", i)); err != nil {
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

func (r *Runner) expandTask(ctx context.Context, task *action.Task, vars map[string]any, planTasks *[]*PlanTask, scope *expansionScope, lineage []string, taskPath []int, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := task.ResolveModule(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	segment := scope.next(taskLineageSegment(task))
	currentLineage := append(append([]string{}, lineage...), segment)

	if task.Uses == "" {
		pt, err := buildPlanTask(task, currentLineage, taskPath, vars)
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
		childTaskPath := append(append([]int{}, taskPath...), j+1)
		if err := r.expandTask(ctx, at, childVars, planTasks, childScope, currentLineage, childTaskPath, childLabel); err != nil {
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
func buildPlanTask(t *action.Task, lineage []string, taskPath []int, vars map[string]any) (*PlanTask, error) {
	id := strings.Join(lineage, "/")
	rawParams := cloneMap(t.Params)
	templateVars := cloneMap(vars)

	return &PlanTask{
		ID:           id,
		TaskPath:     formatTaskPath(taskPath),
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

func formatTaskPath(path []int) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, 0, len(path))
	for _, segment := range path {
		parts = append(parts, strconv.Itoa(segment))
	}
	return strings.Join(parts, ".")
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
	r.emitPhaseStart("stage")
	finishStage := func(err error) error {
		if err != nil {
			r.emitPhaseEnd("stage", "failed", len(plan.Tasks))
			return err
		}
		r.emitPhaseEnd("stage", "ok", len(plan.Tasks))
		return nil
	}

	if r.config.BundleOutputDir == "" {
		return finishStage(fmt.Errorf("stage: bundle output directory is not configured"))
	}

	info, err := r.target.Info(ctx)
	if err != nil {
		return finishStage(fmt.Errorf("stage: target info: %w", err))
	}

	if err := r.validateStagePlan(plan); err != nil {
		return finishStage(err)
	}

	planBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return finishStage(fmt.Errorf("stage: marshal plan: %w", err))
	}

	binaryPath := r.config.BundleBinaryPath
	if binaryPath == "" {
		return finishStage(fmt.Errorf("stage: bundle runtime binary path is not configured"))
	}
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return finishStage(fmt.Errorf("stage: read runtime binary %q: %w", binaryPath, err))
	}

	moduleInfos, pluginFiles, err := r.stageModuleFiles(plan)
	if err != nil {
		return finishStage(err)
	}

	files := []bundle.FileSpec{
		{
			Path: bundle.PlanPath,
			Mode: 0o644,
			Data: planBytes,
		},
		{
			Path: filepath.ToSlash(filepath.Join("runtime", filepath.Base(binaryPath))),
			Mode: 0o755,
			Data: binaryData,
		},
	}
	files = append(files, pluginFiles...)

	lockEntries := []action.LockEntry(nil)
	if r.config.Lockfile != nil {
		lockEntries = make([]action.LockEntry, 0, len(r.config.Lockfile.Actions))
		for _, entry := range r.config.Lockfile.Actions {
			lockEntries = append(lockEntries, entry)
		}
		slices.SortFunc(lockEntries, func(a, b action.LockEntry) int {
			switch {
			case a.Ref < b.Ref:
				return -1
			case a.Ref > b.Ref:
				return 1
			default:
				return 0
			}
		})
	}

	manifest := &bundle.Manifest{
		FormatVersion: 1,
		CreatedAt:     time.Now().UTC(),
		PlaybookName:  plan.PlaybookName,
		TargetName:    r.targetName(),
		TargetOS:      info.OSVersion,
		TargetArch:    info.Arch,
		RuntimeBinary: filepath.ToSlash(filepath.Join("runtime", filepath.Base(binaryPath))),
		Build: bundle.BuildInfo{
			Version: r.config.Version,
			Commit:  r.config.Commit,
			Date:    r.config.BuildDate,
		},
		Modules:     moduleInfos,
		LockEntries: lockEntries,
	}

	bundlePath := filepath.Join(r.config.BundleOutputDir, bundle.BundleFileName(plan.PlaybookName, r.targetName(), info.OSVersion, info.Arch))
	if err := bundle.Write(bundlePath, manifest, files); err != nil {
		return finishStage(fmt.Errorf("stage: %w", err))
	}
	return finishStage(nil)
}

func (r *Runner) validateStagePlan(plan *ExecutionPlan) error {
	if len(plan.Tasks) == 0 {
		return nil
	}

	for _, task := range plan.Tasks {
		if r.config.ModuleRegistry != nil {
			if _, ok := r.config.ModuleRegistry[task.Module]; !ok {
				return fmt.Errorf("stage: task %q references unknown module %q", task.Name, task.Module)
			}
		}
		preview, err := PreviewTask(task, r.config.TargetVars)
		if err != nil {
			return fmt.Errorf("stage: preview task %q: %w", task.Name, err)
		}
		if containsSecretValue(preview.Params) {
			return fmt.Errorf("stage: task %q depends on secret values that cannot be embedded in a staged bundle", preview.Name)
		}
	}
	return nil
}

func (r *Runner) stageModuleFiles(plan *ExecutionPlan) ([]bundle.ModuleInfo, []bundle.FileSpec, error) {
	used := make(map[string]struct{})
	for _, task := range plan.Tasks {
		used[task.Module] = struct{}{}
	}

	pluginIndex := make(map[string]plugins.LoadedPlugin, len(r.config.BundlePlugins))
	for _, plugin := range r.config.BundlePlugins {
		pluginIndex[plugin.Name] = plugin
	}

	moduleNames := make([]string, 0, len(used))
	for name := range used {
		moduleNames = append(moduleNames, name)
	}
	slices.Sort(moduleNames)

	modules := make([]bundle.ModuleInfo, 0, len(moduleNames))
	files := make([]bundle.FileSpec, 0, len(moduleNames))
	for _, name := range moduleNames {
		if plugin, ok := pluginIndex[name]; ok {
			data, err := os.ReadFile(plugin.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("stage: read plugin %q: %w", plugin.Path, err)
			}
			entryPath := filepath.ToSlash(filepath.Join("plugins", filepath.Base(plugin.Path)))
			files = append(files, bundle.FileSpec{
				Path: entryPath,
				Mode: 0o755,
				Data: data,
			})
			modules = append(modules, bundle.ModuleInfo{
				Name:    name,
				Kind:    "plugin",
				Path:    entryPath,
				Version: plugin.Version,
			})
			continue
		}

		if r.config.ModuleRegistry != nil {
			if _, ok := r.config.ModuleRegistry[name]; !ok {
				return nil, nil, fmt.Errorf("stage: task references unknown module %q", name)
			}
		}
		modules = append(modules, bundle.ModuleInfo{
			Name: name,
			Kind: "builtin",
		})
	}

	return modules, files, nil
}

// Apply executes the task graph against the target.
func (r *Runner) Apply(ctx context.Context, plan *ExecutionPlan) error {
	prepared, err := r.GatherContext(ctx)
	if err != nil {
		return err
	}
	return r.ApplyPrepared(ctx, plan, prepared)
}

// PreparedContext holds execution-time context gathered before task execution.
type PreparedContext struct {
	exec *executionContext
}

// GatherContext resolves target info and facts before executing any tasks.
func (r *Runner) GatherContext(ctx context.Context) (*PreparedContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.emitPhaseStart("gather-context")
	execCtx, err := r.buildExecutionContext(ctx)
	if err != nil {
		r.emitPhaseEnd("gather-context", "failed", 0)
		return nil, err
	}
	r.emitPhaseEnd("gather-context", "ok", 0)
	return &PreparedContext{exec: execCtx}, nil
}

// ApplyPrepared executes the task graph using a previously gathered context.
func (r *Runner) ApplyPrepared(ctx context.Context, plan *ExecutionPlan, prepared *PreparedContext) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prepared == nil || prepared.exec == nil {
		return fmt.Errorf("apply: nil prepared context")
	}
	dag, err := BuildDAG(plan.Tasks)
	if err != nil {
		return fmt.Errorf("apply: build DAG: %w", err)
	}

	ordered := dag.TopologicalOrder()
	execCtx := prepared.exec
	r.emitPhaseStart("execute")

	state := &State{
		Tasks: make(map[string]TaskSnapshot),
	}

	// Track outcome counts for the play recap.
	var okCount, changedCount, failedCount, skippedCount int

	// Track which tasks have succeeded for dependency checking.
	succeeded := make(map[string]bool)
	failed := make(map[string]bool)

	finishApply := func() error {
		state.LastApplied = time.Now()
		if !r.config.DryRun && r.config.StatePath != "" {
			if err := state.Save(r.config.StatePath); err != nil {
				r.emitPhaseEnd("execute", "failed", len(ordered))
				return fmt.Errorf("apply: save state: %w", err)
			}
		}
		executeStatus := "ok"
		if failedCount > 0 {
			executeStatus = "failed"
		}
		r.emitPhaseEnd("execute", executeStatus, len(ordered))

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

	for _, pt := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}

		taskName, err := renderTaskName(pt, execCtx)
		if err != nil {
			r.emitPhaseEnd("execute", "failed", len(ordered))
			return fmt.Errorf("apply: task %q: render name: %w", pt.Name, err)
		}
		r.emitTaskStart(pt, taskName, len(ordered))

		// Tag filtering.
		if !r.taskMatchesTags(pt) {
			r.emitTaskResult(pt, taskName, target.StatusSkipped, "tag-filtered")
			state.RecordTask(newTaskSnapshot(pt, taskName, pt.Params, target.StatusSkipped, "tag-filtered", nil))
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
			r.emitTaskResult(pt, taskName, target.StatusSkipped, "dependency-failed")
			state.RecordTask(newTaskSnapshot(pt, taskName, pt.Params, target.StatusSkipped, "dependency-failed", dag))
			skippedCount++
			succeeded[pt.ID] = false
			continue
		}

		// Evaluate when: condition.
		if pt.When != "" {
			ok, err := renderTaskWhen(pt, execCtx)
			if err != nil {
				r.emitPhaseEnd("execute", "failed", len(ordered))
				return fmt.Errorf("apply: task %q: evaluate when condition: %w", pt.Name, err)
			}
			if !ok {
				r.emitTaskResult(pt, taskName, target.StatusSkipped, "when-condition-false")
				state.RecordTask(newTaskSnapshot(pt, taskName, pt.Params, target.StatusSkipped, "when-condition-false", dag))
				skippedCount++
				succeeded[pt.ID] = false
				continue
			}
		}

		params, taskName, err := renderTaskParams(pt, execCtx)
		if err != nil {
			r.emitPhaseEnd("execute", "failed", len(ordered))
			return fmt.Errorf("apply: task %q: render params: %w", pt.Name, err)
		}
		if r.config.Secrets != nil && r.config.Secrets.HasProviders() {
			params, err = r.config.Secrets.ResolveMap(ctx, params)
			if err != nil {
				r.emitPhaseEnd("execute", "failed", len(ordered))
				return fmt.Errorf("apply: task %q: %w", pt.Name, err)
			}
		}

		// Execute the task against the target.
		execTaskCtx := tasklog.WithTask(ctx, r.taskLogSink(), tasklog.Entry{
			Target:   r.targetName(),
			TaskID:   pt.ID,
			TaskPath: pt.TaskPath,
			TaskName: taskName,
			Module:   pt.Module,
		})
		result, execErr := r.target.Execute(execTaskCtx, pt.ID, pt.Module, params, r.config.DryRun)
		if execErr != nil {
			if !pt.IgnoreErrors {
				r.emitTaskResult(pt, taskName, target.StatusFailed, execErr.Error())
				state.RecordTask(newTaskSnapshot(pt, taskName, params, target.StatusFailed, execErr.Error(), dag))
				failedCount++
				failed[pt.ID] = true
				return finishApply()
			}
			// IgnoreErrors: treat as ok.
			result = target.Result{
				TaskID:  pt.ID,
				Status:  target.StatusFailed,
				Message: execErr.Error(),
			}
		}

		state.RecordTask(newTaskSnapshot(pt, taskName, params, result.Status, result.Message, dag))

		r.emitTaskResult(pt, taskName, result.Status, result.Message)

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
				return finishApply()
			} else {
				succeeded[pt.ID] = true
			}
		case target.StatusSkipped:
			skippedCount++
			succeeded[pt.ID] = false
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

// emitTaskStart emits a task_start event to the renderer.
func (r *Runner) emitTaskStart(pt *PlanTask, taskName string, taskTotal int) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:      output.EventTaskStart,
		TaskID:    pt.ID,
		TaskPath:  pt.TaskPath,
		TaskName:  taskName,
		Target:    r.targetName(),
		Module:    pt.Module,
		TaskTotal: taskTotal,
	})
}

// emitTaskResult emits a task_result event to the renderer.
func (r *Runner) emitTaskResult(pt *PlanTask, taskName string, status target.Status, message string) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:     output.EventTaskResult,
		TaskID:   pt.ID,
		TaskPath: pt.TaskPath,
		TaskName: taskName,
		Target:   r.targetName(),
		Module:   pt.Module,
		Status:   string(status),
		Message:  message,
	})
}

func (r *Runner) emitPhaseStart(phase string) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:   output.EventPhaseStart,
		Target: r.targetName(),
		Phase:  phase,
	})
}

func (r *Runner) emitPhaseEnd(phase, status string, taskTotal int) {
	if r.config.Renderer == nil {
		return
	}
	r.config.Renderer.Emit(output.Event{
		Type:      output.EventPhaseEnd,
		Target:    r.targetName(),
		Phase:     phase,
		Status:    status,
		TaskTotal: taskTotal,
	})
}

func (r *Runner) taskLogSink() tasklog.Sink {
	if r.config.Renderer == nil {
		return nil
	}
	return runnerTaskLogSink{renderer: r.config.Renderer}
}

type runnerTaskLogSink struct {
	renderer output.Renderer
}

func (s runnerTaskLogSink) EmitTaskLog(entry tasklog.Entry) {
	if s.renderer == nil {
		return
	}
	s.renderer.Emit(output.Event{
		Type:     output.EventTaskLog,
		Target:   entry.Target,
		TaskID:   entry.TaskID,
		TaskPath: entry.TaskPath,
		TaskName: entry.TaskName,
		Module:   entry.Module,
		Stream:   entry.Stream,
		Line:     entry.Line,
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

func renderTaskName(task *PlanTask, execCtx *executionContext) (string, error) {
	name := task.Name
	if task.Name == "" {
		return name, nil
	}

	eng := template.New(task.TemplateVars).
		WithTarget(execCtx.target).
		WithFacts(execCtx.facts).
		WithEnv(execCtx.env)

	return eng.Render(task.Name)
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
	// Return empty string rather than a hardcoded "localhost" to avoid
	// silently claiming a local identity when no target name is configured.
	return ""
}
