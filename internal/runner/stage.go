package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/bundle"
	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// stage assembles a self-contained artifact bundle (zip).
func (r *Runner) stage(ctx context.Context, plan *ExecutionPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("stage: nil execution plan")
	}

	if r.config.BundleOutputDir == "" {
		return fmt.Errorf("stage: bundle output directory is not configured")
	}

	r.emitActivityStart("connecting")
	info, err := r.target.Info(ctx)
	if err != nil {
		r.emitActivityResult("connecting", "failed")
	} else {
		r.emitActivityResult("connecting", "ok")
	}
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
	return sanitizeSlug(name, "secret")
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
			status := sdk.InspectPlugin(plugin.Path, plugin.Source)
			if status.ErrorMessage != "" {
				return nil, nil, fmt.Errorf("stage: inspect plugin %q: %s", plugin.Path, status.ErrorMessage)
			}
			if status.Name != "" && status.Name != name {
				return nil, nil, fmt.Errorf("stage: plugin %q reported logical name %q, want %q", plugin.Path, status.Name, name)
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
				Version: status.Version,
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
