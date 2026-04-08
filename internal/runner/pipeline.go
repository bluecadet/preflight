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

// Plan resolves all action refs, expands tasks into a flat list, resolves
// variables. Returns an ExecutionPlan. Pure computation — no I/O against targets.
func (r *Runner) Plan(ctx context.Context, playbook *action.Playbook) (*ExecutionPlan, error) {
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

	if r.config.BundleOutputDir == "" {
		return fmt.Errorf("stage: bundle output directory is not configured")
	}

	info, err := r.target.Info(ctx)
	if err != nil {
		return fmt.Errorf("stage: target info: %w", err)
	}

	stageSecrets, err := r.analyzeStagePlan(ctx, plan)
	if err != nil {
		return err
	}

	planBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("stage: marshal plan: %w", err)
	}

	moduleInfos, pluginFiles, err := r.stageModuleFiles(plan)
	if err != nil {
		return err
	}

	files := []bundle.FileSpec{
		{
			Path: bundle.PlanPath,
			Mode: stageSecrets.planMode,
			Data: planBytes,
		},
	}
	files = append(files, stageSecrets.files...)
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
		FormatVersion: bundle.FormatV2,
		CreatedAt:     time.Now().UTC(),
		PlaybookName:  plan.PlaybookName,
		TargetName:    r.targetName(),
		TargetOS:      info.OSVersion,
		TargetArch:    info.Arch,
		Build: bundle.BuildInfo{
			Version: r.config.Version,
			Commit:  r.config.Commit,
			Date:    r.config.BuildDate,
		},
		Modules:       moduleInfos,
		LockEntries:   lockEntries,
		SecretMode:    stageSecrets.mode,
		SecretEntries: stageSecrets.entries,
	}

	bundlePath := filepath.Join(r.config.BundleOutputDir, bundle.BundleFileName(plan.PlaybookName, r.targetName(), info.OSVersion, info.Arch))
	if err := bundle.Write(bundlePath, manifest, files); err != nil {
		return fmt.Errorf("stage: %w", err)
	}
	if stageSecrets.mode == bundle.SecretModePlaintext {
		r.emitWarning("bundle contains plaintext secrets")
	}
	return nil
}

type stageSecretBundle struct {
	mode     bundle.SecretMode
	planMode os.FileMode
	entries  []bundle.SecretEntry
	files    []bundle.FileSpec
}

func (r *Runner) analyzeStagePlan(ctx context.Context, plan *ExecutionPlan) (*stageSecretBundle, error) {
	result := &stageSecretBundle{planMode: 0o644}
	if len(plan.Tasks) == 0 {
		return result, nil
	}

	usedRefs := make(map[string]struct{})
	for _, task := range plan.Tasks {
		if r.config.ModuleRegistry != nil {
			if _, ok := r.config.ModuleRegistry[task.Module]; !ok {
				return nil, fmt.Errorf("stage: task %q references unknown module %q", task.Name, task.Module)
			}
		}
		preview, err := PreviewTask(task, r.config.TargetVars)
		if err != nil {
			return nil, fmt.Errorf("stage: preview task %q: %w", task.Name, err)
		}
		analysis := AnalyzeSecretValues(map[string]any{
			"params": preview.Params,
			"become": preview.Become,
		})
		if analysis.HasLiteralSecrets && !r.config.AllowPlaintextSecretsInBundle {
			return nil, fmt.Errorf("stage: task %q depends on secret values that cannot be embedded in a staged bundle", preview.Name)
		}
		for _, name := range analysis.RefNames {
			usedRefs[name] = struct{}{}
		}
		if analysis.HasLiteralSecrets {
			result.mode = bundle.SecretModePlaintext
			result.planMode = 0o600
		}
	}

	if len(usedRefs) == 0 {
		return result, nil
	}
	refNames := make([]string, 0, len(usedRefs))
	for name := range usedRefs {
		refNames = append(refNames, name)
	}
	slices.Sort(refNames)

	secretMode := bundle.SecretModeEncrypted
	if r.config.AllowPlaintextSecretsInBundle {
		secretMode = bundle.SecretModePlaintext
		result.planMode = 0o600
	}
	entries, files, err := r.stageSecretFiles(ctx, refNames, secretMode)
	if err != nil {
		return nil, err
	}
	result.mode = secretMode
	result.entries = entries
	result.files = files
	return result, nil
}

func (r *Runner) stageSecretFiles(ctx context.Context, names []string, mode bundle.SecretMode) ([]bundle.SecretEntry, []bundle.FileSpec, error) {
	if len(names) == 0 {
		return nil, nil, nil
	}

	entries := make([]bundle.SecretEntry, 0, len(names))
	files := make([]bundle.FileSpec, 0, len(names))
	for _, name := range names {
		entry, ok := r.config.SecretsConfig.Entries[name]
		if !ok {
			return nil, nil, fmt.Errorf("stage: secret %q is not defined in preflight.yml", name)
		}

		var (
			relPath string
			data    []byte
			err     error
		)
		switch mode {
		case bundle.SecretModeEncrypted:
			relPath = stageSecretBundlePath(name, true)
			data, err = os.ReadFile(r.bundleSecretSourcePath(entry.File))
			if err != nil {
				return nil, nil, fmt.Errorf("stage: read encrypted secret %q: %w", name, err)
			}
		case bundle.SecretModePlaintext:
			relPath = stageSecretBundlePath(name, false)
			if r.config.Secrets == nil || !r.config.Secrets.HasProviders() {
				return nil, nil, fmt.Errorf("stage: no secrets resolver is configured")
			}
			resolved, err := r.config.Secrets.ResolveRef(ctx, "secret:"+name)
			if err != nil {
				return nil, nil, fmt.Errorf("stage: resolve secret %q: %w", name, err)
			}
			data = []byte(resolved)
		default:
			return nil, nil, fmt.Errorf("stage: unsupported secret mode %q", mode)
		}

		entries = append(entries, bundle.SecretEntry{Name: name, Path: relPath})
		files = append(files, bundle.FileSpec{
			Path: relPath,
			Mode: 0o600,
			Data: data,
		})
	}
	return entries, files, nil
}

func (r *Runner) bundleSecretSourcePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.config.ProjectDir, path)
}

func stageSecretBundlePath(name string, encrypted bool) string {
	path := filepath.ToSlash(filepath.Join("secrets", sanitizeStageSecretName(name)))
	if encrypted {
		return path + ".age"
	}
	return path
}

func sanitizeStageSecretName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "secret"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	trimmed := strings.Trim(b.String(), "-")
	if trimmed == "" {
		return "secret"
	}
	return trimmed
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
				TaskID:  pt.ID,
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

func renderTaskExecutionOptions(task *PlanTask, execCtx *executionContext) (map[string]any, target.ExecutionOptions, error) {
	if len(task.Become) == 0 {
		return nil, target.ExecutionOptions{}, nil
	}

	eng := template.New(task.TemplateVars).
		WithTarget(execCtx.target).
		WithFacts(execCtx.facts).
		WithEnv(execCtx.env)

	become, err := eng.RenderMap(task.Become)
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
