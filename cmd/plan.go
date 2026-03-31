package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/claytercek/preflight/internal/action"
	"github.com/claytercek/preflight/internal/module"
	"github.com/claytercek/preflight/internal/output"
	"github.com/claytercek/preflight/internal/runner"
	"github.com/claytercek/preflight/internal/target"
	"github.com/spf13/cobra"
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

	varFlags, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varFlags)

	tags, _ := cmd.Flags().GetStringSlice("tags")
	skipTags, _ := cmd.Flags().GetStringSlice("skip-tags")

	pb, err := action.ParsePlaybookFile(playbookPath)
	if err != nil {
		return err
	}

	projectDir, _ := playbookDir(playbookPath)
	chain := action.DefaultChain(projectDir)

	registry := module.Registry()
	tgt := target.NewLocalTarget(registry)

	outFmt := getOutputFormat(cmd)
	renderer := output.New(outFmt, os.Stdout)
	defer renderer.Close()

	cfg := runner.Config{
		DryRun:   false,
		Tags:     tags,
		SkipTags: skipTags,
		Vars:     vars,
		Phase:    "plan",
		Renderer: renderer,
	}

	r := runner.New(tgt, chain, cfg)

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		return err
	}

	fmt.Printf("Playbook: %s\n", plan.PlaybookName)
	fmt.Printf("Tasks (%d):\n", len(plan.Tasks))
	for i, pt := range plan.Tasks {
		fmt.Printf("  %d. [%s] %s", i+1, pt.Module, pt.Name)
		if pt.When != "" {
			fmt.Printf(" (when: %s)", pt.When)
		}
		if len(pt.Tags) > 0 {
			fmt.Printf(" [tags: %v]", pt.Tags)
		}
		fmt.Println()
	}

	return nil
}
