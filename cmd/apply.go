package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/bundle"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

var applyCmd = &cobra.Command{
	Use:   "apply <playbook>",
	Short: "Apply a playbook to targets",
	Args:  cobra.ArbitraryArgs,
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().String("bundle", "", "apply from a staged bundle zip")
	applyCmd.Flags().String("bundle-output-dir", "", "directory for staged bundle zips")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	return runPlaybook(cmd, args, false)
}

// runPlaybook is the shared implementation for apply and check.
func runPlaybook(cmd *cobra.Command, args []string, dryRun bool) error {
	bundlePath, _ := cmd.Flags().GetString("bundle")
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

	// --check flag overrides the dryRun argument.
	checkFlag, _ := cmd.Flags().GetBool("check")
	if checkFlag {
		dryRun = true
	}
	commandName := "apply"
	if dryRun {
		commandName = "check"
	}
	if phase == "stage" {
		commandName = "stage"
	}

	outFmt := getOutputFormat(cmd)
	verbose, _ := cmd.Flags().GetBool("verbose")
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, output.Options{
		Verbose:   verbose,
		Input:     os.Stdin,
		Interrupt: cancel,
		Command:   commandName,
	}))
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
	if renderer != nil {
		for _, host := range hosts {
			renderer.Emit(output.Event{
				Type:     output.EventPlayStart,
				PlayName: pb.Name,
				Target:   host.Name,
			})
		}
	}
	emitRunError := func(err error) error {
		if err == nil || renderer == nil {
			return err
		}
		renderer.Emit(output.Event{
			Type:  output.EventError,
			Error: err,
		})
		return err
	}

	if phase != "plan" {
		if renderer != nil {
			renderer.Emit(output.Event{
				Type:  output.EventPhaseStart,
				Phase: "fetch",
			})
		}
		// Fetch is target-agnostic: it only resolves action refs via the resolver
		// chain and never calls any method on the target. A nil target is safe here.
		fetchRunner := runner.New(nil, chain, runner.Config{})
		if err := fetchRunner.Fetch(ctx, pb); err != nil {
			if renderer != nil {
				renderer.Emit(output.Event{
					Type:   output.EventPhaseEnd,
					Phase:  "fetch",
					Status: "failed",
				})
			}
			return emitRunError(err)
		}
		if renderer != nil {
			renderer.Emit(output.Event{
				Type:   output.EventPhaseEnd,
				Phase:  "fetch",
				Status: "ok",
			})
		}
	}
	if phase == "fetch" {
		return nil
	}

	type plannedHost struct {
		host   targeting.ResolvedHost
		runner *runner.Runner
		plan   *runner.ExecutionPlan
	}
	planned := make(map[string]plannedHost, len(hosts))
	var plannedMu sync.Mutex

	err = runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		cfg := runner.Config{
			DryRun:           dryRun,
			Tags:             tags,
			SkipTags:         skipTags,
			Concurrency:      concurrency,
			ProjectDir:       projectDir,
			ProjectVars:      projectCfg.Vars,
			InventoryVars:    host.Vars,
			Vars:             vars,
			TargetVars:       host.TargetVars,
			TargetName:       host.Name,
			Phase:            phase,
			Renderer:         renderer,
			Secrets:          secretsResolver,
			StatePath:        host.StatePath,
			ModuleRegistry:   registry,
			BundleOutputDir:  bundleOutputDir(cmd, projectDir),
			BundleBinaryPath: currentBinaryPath(),
			BundlePlugins:    loadedPlugins,
			Lockfile:         lockfile,
			Version:          buildVersion,
			Commit:           buildCommit,
			BuildDate:        buildDate,
		}

		if renderer != nil {
			renderer.Emit(output.Event{
				Type:   output.EventPhaseStart,
				Target: host.Name,
				Phase:  "plan",
			})
		}

		r := runner.New(host.Target, chain, cfg)
		plan, err := r.Plan(runCtx, pb)
		if err != nil {
			if renderer != nil {
				renderer.Emit(output.Event{
					Type:   output.EventPhaseEnd,
					Target: host.Name,
					Phase:  "plan",
					Status: "failed",
				})
			}
			return fmt.Errorf("plan for %s: %w", host.Name, err)
		}
		if renderer != nil {
			renderer.Emit(output.Event{
				Type:      output.EventPhaseEnd,
				Target:    host.Name,
				Phase:     "plan",
				Status:    "ok",
				TaskTotal: len(plan.Tasks),
			})
		}
		plannedMu.Lock()
		planned[host.Name] = plannedHost{
			host:   host,
			runner: r,
			plan:   plan,
		}
		plannedMu.Unlock()
		return nil
	})
	if err != nil {
		return emitRunError(err)
	}
	if phase == "plan" {
		return nil
	}

	if phase == "stage" {
		err = runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
			plannedHost, ok := planned[host.Name]
			if !ok {
				return fmt.Errorf("stage for %s: missing planned host state", host.Name)
			}
			if err := plannedHost.runner.Stage(runCtx, plannedHost.plan); err != nil {
				return fmt.Errorf("stage for %s: %w", host.Name, err)
			}
			return nil
		})
		return emitRunError(err)
	}

	prepared := make(map[string]*runner.PreparedContext, len(hosts))
	var preparedMu sync.Mutex
	err = runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		plannedHost, ok := planned[host.Name]
		if !ok {
			return fmt.Errorf("gather context for %s: missing planned host state", host.Name)
		}
		preparedCtx, err := plannedHost.runner.GatherContext(runCtx)
		if err != nil {
			return fmt.Errorf("gather context for %s: %w", host.Name, err)
		}
		preparedMu.Lock()
		prepared[host.Name] = preparedCtx
		preparedMu.Unlock()
		return nil
	})
	if err != nil {
		return emitRunError(err)
	}

	err = runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		plannedHost, ok := planned[host.Name]
		if !ok {
			return fmt.Errorf("apply for %s: missing planned host state", host.Name)
		}
		preparedCtx, ok := prepared[host.Name]
		if !ok {
			return fmt.Errorf("apply for %s: missing prepared context", host.Name)
		}
		if err := plannedHost.runner.ApplyPrepared(runCtx, plannedHost.plan, preparedCtx); err != nil {
			return fmt.Errorf("apply for %s: %w", host.Name, err)
		}
		return nil
	})
	return emitRunError(err)
}

func runBundleApply(cmd *cobra.Command, bundlePath string, dryRun bool) error {
	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	checkFlag, _ := cmd.Flags().GetBool("check")
	if checkFlag {
		dryRun = true
	}
	commandName := "apply"
	if dryRun {
		commandName = "check"
	}

	outFmt := getOutputFormat(cmd)
	verbose, _ := cmd.Flags().GetBool("verbose")
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, output.Options{
		Verbose:   verbose,
		Input:     os.Stdin,
		Interrupt: cancel,
		Command:   commandName,
	}))
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

	// TargetName is set from the bundle manifest so the state file and recap
	// output correctly identify the original target, not the local machine.
	if renderer != nil {
		renderer.Emit(output.Event{
			Type:     output.EventPlayStart,
			PlayName: plan.PlaybookName,
			Target:   extracted.Manifest.TargetName,
		})
	}
	r := runner.New(target.NewLocalTarget(registry), nil, runner.Config{
		DryRun:         dryRun,
		Renderer:       renderer,
		StatePath:      stateFilePath(cmd),
		TargetName:     extracted.Manifest.TargetName,
		ModuleRegistry: registry,
		Version:        extracted.Manifest.Build.Version,
		Commit:         extracted.Manifest.Build.Commit,
		BuildDate:      extracted.Manifest.Build.Date,
	})
	return r.Apply(ctx, &plan)
}
