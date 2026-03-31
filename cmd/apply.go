package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/targeting"
)

var applyCmd = &cobra.Command{
	Use:   "apply <playbook>",
	Short: "Apply a playbook to targets",
	Args:  cobra.ExactArgs(1),
	RunE:  runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	return runPlaybook(cmd, args, false)
}

// runPlaybook is the shared implementation for apply and check.
func runPlaybook(cmd *cobra.Command, args []string, dryRun bool) error {
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

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.New(outFmt, os.Stdout))
	defer renderer.Close()

	pb, projectDir, projectCfg, secretsResolver, chain, err := loadPlaybookRunContext(playbookPath)
	if err != nil {
		return err
	}

	registry := module.Registry()
	hosts, err := resolveRunHosts(ctx, cmd, projectDir, registry, secretsResolver)
	if err != nil {
		return err
	}

	if phase != "plan" {
		fetchRunner := runner.New(hosts[0].Target, chain, runner.Config{})
		if err := fetchRunner.Fetch(ctx, pb); err != nil {
			return err
		}
	}
	if phase == "fetch" {
		return nil
	}

	return runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		cfg := runner.Config{
			DryRun:        dryRun,
			Tags:          tags,
			SkipTags:      skipTags,
			Concurrency:   concurrency,
			ProjectDir:    projectDir,
			ProjectVars:   projectCfg.Vars,
			InventoryVars: host.Vars,
			Vars:          vars,
			TargetVars:    host.TargetVars,
			TargetName:    host.Name,
			Phase:         phase,
			Renderer:      renderer,
			Secrets:       secretsResolver,
			StatePath:     host.StatePath,
		}

		if renderer != nil {
			renderer.Emit(output.Event{
				Type:     output.EventPlayStart,
				PlayName: pb.Name,
				Target:   host.Name,
			})
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
