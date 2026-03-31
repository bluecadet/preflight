package cmd

import (
	"context"
	"os"

	"github.com/claytercek/preflight/internal/action"
	"github.com/claytercek/preflight/internal/module"
	"github.com/claytercek/preflight/internal/output"
	"github.com/claytercek/preflight/internal/runner"
	"github.com/claytercek/preflight/internal/target"
	"github.com/spf13/cobra"
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
	renderer := output.New(outFmt, os.Stdout)
	defer renderer.Close()

	// Parse the playbook.
	pb, err := action.ParsePlaybookFile(playbookPath)
	if err != nil {
		return err
	}

	// Build the resolver chain using the directory containing the playbook.
	projectDir, _ := playbookDir(playbookPath)
	chain := action.DefaultChain(projectDir)

	// Build the local target.
	registry := module.Registry()
	tgt := target.NewLocalTarget(registry)

	// Build runner config.
	cfg := runner.Config{
		DryRun:      dryRun,
		Tags:        tags,
		SkipTags:    skipTags,
		Concurrency: concurrency,
		Vars:        vars,
		Phase:       phase,
		Renderer:    renderer,
	}

	r := runner.New(tgt, chain, cfg)
	return r.Run(context.Background(), pb)
}

// playbookDir returns the directory containing the playbook file.
func playbookDir(playbookPath string) (string, error) {
	abs, err := os.Getwd()
	if err != nil {
		return ".", err
	}
	// If the playbookPath has a directory component, use it.
	if idx := lastSlash(playbookPath); idx >= 0 {
		return playbookPath[:idx], nil
	}
	return abs, nil
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' || s[i] == '\\' {
			return i
		}
	}
	return -1
}
