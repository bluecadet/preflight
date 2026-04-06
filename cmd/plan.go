package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/runner"
)

var planCmd = &cobra.Command{
	Use:   "plan <playbook>",
	Short: "Resolve and print the execution plan without running it",
	Args:  cobra.ExactArgs(1),
	RunE:  runPlan,
}

func init() {
	planCmd.Flags().String("phase", "", "run only up to this phase: plan, fetch, stage, or apply")
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	playbookPath := getPlaybookPath(args)
	if err := validateLocalOnlyRunFlags(cmd); err != nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	varFlags, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varFlags)

	tags, _ := cmd.Flags().GetStringSlice("tags")
	skipTags, _ := cmd.Flags().GetStringSlice("skip-tags")

	pb, projectDir, projectCfg, secretsResolver, chain, err := loadPlaybookRunContext(playbookPath)
	if err != nil {
		return err
	}

	registry, _, err := buildModuleRegistry(projectDir)
	if err != nil {
		return err
	}
	hosts, err := resolveRunHosts(ctx, cmd, projectDir, registry, secretsResolver)
	if err != nil {
		return err
	}

	for idx, host := range hosts {
		cfg := runner.Config{
			DryRun:         false,
			Tags:           tags,
			SkipTags:       skipTags,
			ProjectVars:    projectCfg.Vars,
			InventoryVars:  host.Vars,
			Vars:           vars,
			TargetVars:     host.TargetVars,
			TargetName:     host.Name,
			Phase:          "plan",
			Secrets:        secretsResolver,
			ModuleRegistry: registry,
		}

		r := runner.New(host.Target, chain, cfg)
		plan, err := r.Plan(ctx, pb)
		if err != nil {
			return fmt.Errorf("plan for %s: %w", host.Name, err)
		}

		if idx > 0 {
			fmt.Println()
		}
		fmt.Printf("Target: %s\n", host.Name)
		fmt.Printf("Playbook: %s\n", plan.PlaybookName)
		fmt.Printf("Tasks (%d):\n", len(plan.Tasks))
		for i, pt := range plan.Tasks {
			preview, err := runner.PreviewTask(pt, host.TargetVars)
			if err != nil {
				return fmt.Errorf("preview task %q for %s: %w", pt.Name, host.Name, err)
			}
			fmt.Printf("  %d. [%s] %s", i+1, preview.Module, preview.Name)
			if preview.When != "" {
				fmt.Printf(" (when: %s)", preview.When)
			}
			if len(preview.Tags) > 0 {
				fmt.Printf(" [tags: %v]", preview.Tags)
			}
			fmt.Println()
		}
	}

	return nil
}
