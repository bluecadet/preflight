package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	addTargetingFlags(applyCmd)
	addVarFlags(applyCmd)
	addTagFlags(applyCmd)
	addOutputFlags(applyCmd)
	addConcurrencyFlag(applyCmd)
	addTimeoutFlag(applyCmd)
	applyCmd.Flags().String("bundle", "", "apply from a staged bundle zip")
	applyCmd.Flags().String("secret-identity", "", "path to an age identity file used to decrypt bundled encrypted secrets")
	applyCmd.Flags().String("state-file", "", "path to state file (default: "+defaultStatePath+")")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	return runPlaybook(cmd, args, playbookRunOptions{})
}

type playbookRunOptions struct {
	dryRun    bool
	stageOnly bool
}

// runPlaybook is the shared implementation for apply, check, and stage.
func runPlaybook(cmd *cobra.Command, args []string, opts playbookRunOptions) error {
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
		return runBundleApply(cmd, bundlePath, opts.dryRun)
	}
	if len(args) != 1 {
		return fmt.Errorf("apply: expected exactly one playbook path")
	}

	playbookPath := getPlaybookPath(args)
	if err := validateConcurrency(cmd); err != nil {
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
	if allowPlaintextSecrets && !opts.stageOnly {
		return fmt.Errorf("apply: --allow-plaintext-secrets-in-bundle requires the stage command")
	}

	session, err := newPlaybookSession(ctx, playbookPath, false)
	if err != nil {
		return err
	}
	hosts, err := resolveRunHosts(ctx, cmd, session.ProjectCfg, session.Registry, session.Secrets)
	if err != nil {
		return err
	}

	// Set up the fan-out bus: terminal renderer + disk run log.
	runID := output.RunID()
	runLogPath := filepath.Join(session.ProjectDir, output.RunDir(runID), "run.jsonl")
	runLogSink, err := output.NewRunLogSink(runID, runLogPath)
	if err != nil {
		return fmt.Errorf("create run log: %w", err)
	}
	termRenderer := newRenderer(cmd)
	bus := output.NewBus(termRenderer, runLogSink)
	defer bus.Close()

	// Emit the version event.
	bus.Emit(output.VersionEvent{
		SchemaVersion:    "1.0",
		PreflightVersion: buildVersion,
		PlaybookName:     session.Playbook.Name,
	})

	runStartTime := time.Now()
	bus.Emit(output.RunStartEvent{
		Mode:         runMode(opts),
		PlaybookPath: playbookPath,
		PlaybookName: session.Playbook.Name,
		Targets:      displayTargetNames(hosts),
		DryRun:       opts.dryRun,
		Tags:         tags,
		SkipTags:     skipTags,
	})
	if err := runner.New(nil, session.Chain, runner.Config{}).Fetch(ctx, session.Playbook); err != nil {
		bus.Emit(output.ErrorEvent{Message: fmt.Sprintf("fetch phase failed: %v", err)})
		return err
	}

	hostErrors := runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		bundleDir, err := bundleOutputDir(cmd, session.ProjectDir)
		if err != nil {
			return fmt.Errorf("resolve bundle output dir: %w", err)
		}
		cfg := runner.Config{
			DryRun:                        opts.dryRun,
			Tags:                          tags,
			SkipTags:                      skipTags,
			Concurrency:                   concurrency,
			ProjectDir:                    session.ProjectDir,
			ProjectName:                   session.ProjectCfg.Project,
			ProjectEnv:                    session.ProjectCfg.Environment,
			ProjectVars:                   session.ProjectCfg.Vars,
			InventoryVars:                 host.Vars,
			Vars:                          vars,
			TargetVars:                    host.TargetVars,
			TargetName:                    host.Name,
			Renderer:                      bus,
			SkipFetch:                     true,
			Secrets:                       session.Secrets,
			SecretsConfig:                 session.ProjectCfg.Secrets,
			StatePath:                     host.StatePath,
			ModuleRegistry:                session.Registry,
			BundleOutputDir:               bundleDir,
			BundlePlugins:                 session.LoadedPlugins,
			AllowPlaintextSecretsInBundle: allowPlaintextSecrets,
			Lockfile:                      session.Lockfile,
			Version:                       buildVersion,
			Commit:                        buildCommit,
			BuildDate:                     buildDate,
		}
		if opts.stageOnly {
			cfg.Phase = "stage"
		}

		r := runner.New(host.Target, session.Chain, cfg)
		if err := r.Run(runCtx, session.Playbook); err != nil {
			action := "apply"
			if opts.stageOnly {
				action = "stage"
			}
			return wrapHostLabelError(action, action, host.Name, err)
		}
		return nil
	})

	// Emit run summary.
	elapsedMs := time.Since(runStartTime).Milliseconds()
	bus.Emit(output.RunSummaryEvent{
		Status:   runStatus(hostErrors),
		OKCount:  0,
		ChangedCount: 0,
		FailedCount:  0,
		SkippedCount: 0,
		ElapsedMs:    elapsedMs,
	})

	// Write run status files.
	runDir := filepath.Join(session.ProjectDir, output.RunDir(runID))
	_ = output.WriteStatusFile(runDir, runStatus(hostErrors), runExitCode(hostErrors))

	return hostErrors
}

func runStatus(err error) string {
	if err == nil {
		return "success"
	}
	return "failed"
}

func runExitCode(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

func runBundleApply(cmd *cobra.Command, bundlePath string, dryRun bool) error {
	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	renderer := newRenderer(cmd)
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
	renderer.Emit(output.RunStartEvent{
		Mode:         runMode(playbookRunOptions{dryRun: dryRun}),
		PlaybookPath: bundlePath,
		PlaybookName: plan.PlaybookName,
		Targets:      []string{displayTargetName(extracted.Manifest.TargetName, target.TransportLocal)},
	})
	statePath, err := stateFilePath(cmd)
	if err != nil {
		return fmt.Errorf("apply bundle: %w", err)
	}
	r := runner.New(target.NewLocalTarget(registry), nil, runner.Config{
		DryRun:         dryRun,
		Renderer:       renderer,
		Secrets:        secretResolver,
		StatePath:      statePath,
		TargetName:     extracted.Manifest.TargetName,
		ModuleRegistry: registry,
		Version:        extracted.Manifest.Build.Version,
		Commit:         extracted.Manifest.Build.Commit,
		BuildDate:      extracted.Manifest.Build.Date,
	})
	return r.Apply(ctx, &plan)
}

func runMode(opts playbookRunOptions) string {
	switch {
	case opts.stageOnly:
		return "stage"
	case opts.dryRun:
		return "check"
	default:
		return "apply"
	}
}

func displayTargetNames(hosts []targeting.ResolvedHost) []string {
	names := make([]string, 0, len(hosts))
	for _, host := range hosts {
		transport := target.Transport("")
		if host.Target != nil {
			transport = host.Target.Transport()
		}
		names = append(names, displayTargetName(host.Name, transport))
	}
	return names
}

func displayTargetName(name string, transport target.Transport) string {
	if transport == target.TransportLocal {
		return "local"
	}
	if name == "" {
		return "local"
	}
	return name
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
