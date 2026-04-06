package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/bundle"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

var applyCmd = &cobra.Command{
	Use:   "apply <playbook>",
	Short: "Apply a playbook to targets",
	Args:  cobra.RangeArgs(0, 1),
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().String("bundle", "", "apply from a staged bundle zip")
	applyCmd.Flags().String("bundle-output-dir", "", "directory for staged bundle zips")
	applyCmd.Flags().String("secret-identity", "", "path to an age identity file used to decrypt bundled encrypted secrets")
	applyCmd.Flags().Bool("allow-plaintext-secrets-in-bundle", false, "allow staging bundles that contain plaintext secrets")
	applyCmd.Flags().String("phase", "", "run only up to this phase: plan, fetch, stage, or apply")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	return runPlaybook(cmd, args, false)
}

// runPlaybook is the shared implementation for apply and check.
func runPlaybook(cmd *cobra.Command, args []string, dryRun bool) error {
	bundlePath, _ := cmd.Flags().GetString("bundle")
	secretIdentity, _ := cmd.Flags().GetString("secret-identity")
	allowPlaintextSecrets, _ := cmd.Flags().GetBool("allow-plaintext-secrets-in-bundle")
	if secretIdentity != "" && bundlePath == "" {
		return fmt.Errorf("apply: --secret-identity requires --bundle")
	}
	if allowPlaintextSecrets && bundlePath != "" {
		return fmt.Errorf("apply: --allow-plaintext-secrets-in-bundle is only valid when staging a playbook")
	}
	if bundlePath != "" {
		if len(args) > 0 {
			return fmt.Errorf("apply: playbook path and --bundle cannot be used together")
		}
		return runBundleApply(cmd, bundlePath, dryRun)
	}
	if len(args) != 1 {
		return fmt.Errorf("apply: expected exactly one playbook path")
	}

	playbookPath := getPlaybookPath(args)
	if err := validateLocalOnlyRunFlags(cmd); err != nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	// Parse global flags.
	varFlags, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varFlags)

	tags, _ := cmd.Flags().GetStringSlice("tags")
	skipTags, _ := cmd.Flags().GetStringSlice("skip-tags")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	phase, _ := cmd.Flags().GetString("phase")
	if allowPlaintextSecrets && phase != "stage" {
		return fmt.Errorf("apply: --allow-plaintext-secrets-in-bundle requires the stage phase")
	}

	// --check flag overrides the dryRun argument.
	checkFlag, _ := cmd.Flags().GetBool("check")
	if checkFlag {
		dryRun = true
	}

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

	pb, projectDir, projectCfg, secretsResolver, chain, err := loadPlaybookRunContext(playbookPath)
	if err != nil {
		return err
	}

	registry, loadedPlugins, err := buildModuleRegistry(projectDir)
	if err != nil {
		return err
	}
	lockfile, err := loadProjectLockfile(projectDir)
	if err != nil {
		return err
	}
	hosts, err := resolveRunHosts(ctx, cmd, projectDir, registry, secretsResolver)
	if err != nil {
		return err
	}

	if phase != "plan" {
		// Fetch is target-agnostic: it only resolves action refs via the resolver
		// chain and never calls any method on the target. A nil target is safe here.
		fetchRunner := runner.New(nil, chain, runner.Config{})
		if err := fetchRunner.Fetch(ctx, pb); err != nil {
			return err
		}
	}
	if phase == "fetch" {
		return nil
	}

	return runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		cfg := runner.Config{
			DryRun:                        dryRun,
			Tags:                          tags,
			SkipTags:                      skipTags,
			Concurrency:                   concurrency,
			ProjectDir:                    projectDir,
			ProjectVars:                   projectCfg.Vars,
			InventoryVars:                 host.Vars,
			Vars:                          vars,
			TargetVars:                    host.TargetVars,
			TargetName:                    host.Name,
			Phase:                         phase,
			Renderer:                      renderer,
			Secrets:                       secretsResolver,
			SecretsConfig:                 projectCfg.Secrets,
			StatePath:                     host.StatePath,
			ModuleRegistry:                registry,
			BundleOutputDir:               bundleOutputDir(cmd, projectDir),
			BundleBinaryPath:              currentBinaryPath(),
			BundlePlugins:                 loadedPlugins,
			AllowPlaintextSecretsInBundle: allowPlaintextSecrets,
			Lockfile:                      lockfile,
			Version:                       buildVersion,
			Commit:                        buildCommit,
			BuildDate:                     buildDate,
		}

		if renderer != nil {
			renderer.Emit(output.PlayStartEvent{PlayName: pb.Name})
		}

		r := runner.New(host.Target, chain, cfg)
		plan, err := r.Plan(runCtx, pb)
		if err != nil {
			return fmt.Errorf("plan for %s: %w", host.Name, err)
		}

		if phase == "stage" {
			if err := r.Stage(runCtx, plan); err != nil {
				return fmt.Errorf("stage for %s: %w", host.Name, err)
			}
			return nil
		}

		if err := r.Apply(runCtx, plan); err != nil {
			return fmt.Errorf("apply for %s: %w", host.Name, err)
		}
		return nil
	})
}

func runBundleApply(cmd *cobra.Command, bundlePath string, dryRun bool) error {
	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

	extracted, err := bundle.Extract(bundlePath)
	if err != nil {
		return fmt.Errorf("apply bundle: %w", err)
	}
	defer func() {
		_ = extracted.Cleanup()
	}()

	registry, _, err := buildModuleRegistryWithOptions(extracted.RootDir, true, extracted.PluginDir)
	if err != nil {
		return fmt.Errorf("apply bundle: %w", err)
	}

	planBytes, err := os.ReadFile(extracted.PlanPath)
	if err != nil {
		return fmt.Errorf("apply bundle: read plan: %w", err)
	}

	var plan runner.ExecutionPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return fmt.Errorf("apply bundle: parse plan: %w", err)
	}

	checkFlag, _ := cmd.Flags().GetBool("check")
	if checkFlag {
		dryRun = true
	}
	secretIdentity, _ := cmd.Flags().GetString("secret-identity")
	secretResolver, err := buildBundleSecretsResolver(extracted, secretIdentity)
	if err != nil {
		return fmt.Errorf("apply bundle: %w", err)
	}

	// TargetName is set from the bundle manifest so the state file and recap
	// output correctly identify the original target, not the local machine.
	if extracted.Manifest.SecretMode == bundle.SecretModePlaintext && renderer != nil {
		renderer.Emit(output.WarningEvent{Message: "bundle contains plaintext secrets"})
	}
	if renderer != nil {
		renderer.Emit(output.PlayStartEvent{PlayName: plan.PlaybookName})
	}
	r := runner.New(target.NewLocalTarget(registry), nil, runner.Config{
		DryRun:         dryRun,
		Renderer:       renderer,
		Secrets:        secretResolver,
		StatePath:      stateFilePath(cmd),
		TargetName:     extracted.Manifest.TargetName,
		ModuleRegistry: registry,
		Version:        extracted.Manifest.Build.Version,
		Commit:         extracted.Manifest.Build.Commit,
		BuildDate:      extracted.Manifest.Build.Date,
	})
	return r.Apply(ctx, &plan)
}

func buildBundleSecretsResolver(extracted *bundle.ExtractedBundle, identityPath string) (*secrets.Resolver, error) {
	if extracted == nil || extracted.Manifest == nil {
		return secrets.NewResolver(nil), nil
	}
	if extracted.Manifest.SecretMode != bundle.SecretModeEncrypted && extracted.Manifest.SecretMode != bundle.SecretModePlaintext {
		return secrets.NewResolver(nil), nil
	}
	if len(extracted.Manifest.SecretEntries) == 0 {
		return secrets.NewResolver(nil), nil
	}
	if extracted.Manifest.SecretMode == bundle.SecretModeEncrypted && identityPath == "" {
		return nil, fmt.Errorf("--secret-identity is required for encrypted bundle secrets")
	}
	entries := make(map[string]string, len(extracted.Manifest.SecretEntries))
	for _, entry := range extracted.Manifest.SecretEntries {
		entries[entry.Name] = entry.Path
	}
	provider := secrets.NewBundleProvider(
		extracted.RootDir,
		extracted.Manifest.SecretMode == bundle.SecretModeEncrypted,
		identityPath,
		entries,
	)
	return secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: provider,
	}), nil
}
