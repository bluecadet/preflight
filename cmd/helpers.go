package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

var newActionChain = action.DefaultChain

// parseVars converts a slice of "key=value" strings into a map.
// Values without "=" are stored as empty strings.
func parseVars(varFlags []string) map[string]any {
	result := make(map[string]any, len(varFlags))
	for _, kv := range varFlags {
		key, value, found := strings.Cut(kv, "=")
		if !found {
			result[key] = ""
			continue
		}
		result[key] = value
	}
	return result
}

// getOutputFormat reads the --output flag and returns the corresponding Format.
// When the flag is not set (or is the default "text"), AutoDetect is called to
// automatically use FormatTUI when running interactively.
func getOutputFormat(cmd *cobra.Command) output.Format {
	if cmd == nil {
		return output.FormatText
	}
	f, _ := cmd.Flags().GetString("output")
	switch output.Format(f) {
	case output.FormatJSON, output.FormatJSONL:
		return output.Format(f)
	case output.FormatTUI:
		return output.FormatTUI
	default:
		return output.AutoDetect(os.Stdout)
	}
}

// getPlaybookPath returns the first element of args (the playbook path).
func getPlaybookPath(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func projectConfigPath(projectDir string) string {
	return filepath.Join(projectDir, config.FileName)
}

func loadProjectConfig(projectDir string) (*config.Config, error) {
	return config.LoadOptional(projectConfigPath(projectDir))
}

func buildSecretsResolver(projectDir string, cfg *config.Config) *secrets.Resolver {
	if cfg == nil || len(cfg.Secrets.Entries) == 0 {
		return secrets.NewResolver(nil)
	}
	return secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: secrets.NewRepoProvider(projectDir, cfg.Secrets),
	})
}

func loadPlaybookRunContext(playbookPath string) (*action.Playbook, string, *config.Config, *secrets.Resolver, action.Chain, error) {
	pb, err := action.LoadPlaybookFile(playbookPath)
	if err != nil {
		return nil, "", nil, nil, nil, err
	}

	projectDir, err := playbookDir(playbookPath)
	if err != nil {
		return nil, "", nil, nil, nil, err
	}

	projectCfg, err := loadProjectConfig(projectDir)
	if err != nil {
		return nil, "", nil, nil, nil, err
	}

	secretsResolver := buildSecretsResolver(projectDir, projectCfg)
	return pb, projectDir, projectCfg, secretsResolver, newActionChain(projectDir), nil
}

func loadInventoryRunContext(inventoryPath string) (*inventory.Inventory, string, *config.Config, *secrets.Resolver, error) {
	projectDir, err := projectDirForPath(inventoryPath)
	if err != nil {
		return nil, "", nil, nil, err
	}

	projectCfg, err := loadProjectConfig(projectDir)
	if err != nil {
		return nil, "", nil, nil, err
	}

	inv, err := inventory.ParseFile(inventoryPath)
	if err != nil {
		return nil, "", nil, nil, err
	}

	return inv, projectDir, projectCfg, buildSecretsResolver(projectDir, projectCfg), nil
}

func fetchPlaybookActionRefs(ctx context.Context, pb *action.Playbook, chain action.Chain) error {
	if pb == nil {
		return nil
	}
	return runner.New(nil, chain, runner.Config{}).Fetch(ctx, pb)
}

func currentBinaryDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func currentBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return exe
}

func buildModuleRegistry(projectDir string, preferredPluginDirs ...string) (target.ModuleRegistry, []plugins.LoadedPlugin, error) {
	return buildModuleRegistryWithOptions(projectDir, false, preferredPluginDirs...)
}

func buildModuleRegistryWithOptions(projectDir string, exclusive bool, preferredPluginDirs ...string) (target.ModuleRegistry, []plugins.LoadedPlugin, error) {
	return plugins.BuildRegistry(module.Registry(), plugins.Options{
		BinaryDir:              currentBinaryDir(),
		WorkingDir:             projectDir,
		PreferredDirs:          preferredPluginDirs,
		ExclusivePreferredDirs: exclusive,
	})
}

func loadProjectLockfile(projectDir string) (*action.Lockfile, error) {
	if projectDir == "" {
		return &action.Lockfile{Actions: make(map[string]action.LockEntry)}, nil
	}
	return action.LoadLockfile(filepath.Join(projectDir, action.LockfileName))
}

func bundleOutputDir(cmd *cobra.Command, projectDir string) string {
	p, _ := cmd.Flags().GetString("bundle-output-dir")
	if p != "" {
		return p
	}
	if projectDir != "" {
		return filepath.Join(projectDir, "dist", "bundles")
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "dist", "bundles")
}

func playbookDir(playbookPath string) (string, error) {
	dir, err := projectDirForPath(playbookPath)
	if err != nil {
		return "", fmt.Errorf("resolve project dir for %q: %w", playbookPath, err)
	}
	return dir, nil
}

func projectDirForPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return discoverProjectDir(filepath.Dir(abs))
}

func discoverProjectDir(start string) (string, error) {
	current := filepath.Clean(start)
	for {
		if hasProjectMarker(current) {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return start, nil
		}
		current = parent
	}
}

func hasProjectMarker(dir string) bool {
	markers := []string{
		filepath.Join(dir, config.FileName),
		filepath.Join(dir, action.LockfileName),
		filepath.Join(dir, "actions"),
		filepath.Join(dir, ".git"),
	}
	for _, marker := range markers {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}
