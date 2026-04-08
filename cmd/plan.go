package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
)

var planCmd = &cobra.Command{
	Use:   "plan <playbook>",
	Short: "Resolve and print the execution plan without running it",
	Args:  cobra.ExactArgs(1),
	RunE:  runPlan,
}

func init() {
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

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

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

	for _, host := range hosts {
		cfg := runner.Config{
			DryRun:         false,
			Tags:           tags,
			SkipTags:       skipTags,
			ProjectName:    projectCfg.Project,
			ProjectEnv:     projectCfg.Environment,
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

		tasks := make([]output.PlanTaskEntry, 0, len(plan.Tasks))
		for i, pt := range plan.Tasks {
			preview, err := runner.PreviewTask(pt, host.TargetVars)
			if err != nil {
				return fmt.Errorf("preview task %q for %s: %w", pt.Name, host.Name, err)
			}
			tasks = append(tasks, output.PlanTaskEntry{
				Number: i + 1,
				Module: preview.Module,
				Name:   preview.Name,
				When:   preview.When,
				Tags:   preview.Tags,
			})
		}

		renderer.Emit(output.PlanEvent{
			Target:       host.Name,
			PlaybookName: plan.PlaybookName,
			Tasks:        tasks,
		})
	}

	return nil
}
