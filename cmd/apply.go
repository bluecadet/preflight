package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/target"
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
	renderer := output.New(outFmt, os.Stdout)
	defer renderer.Close()

	pb, projectDir, projectCfg, secretsResolver, chain, err := loadPlaybookRunContext(playbookPath)
	if err != nil {
		return err
	}

	// Build the local target.
	registry := module.Registry()
	tgt := target.NewLocalTarget(registry)

	// Build runner config.
	cfg := runner.Config{
		DryRun:      dryRun,
		Tags:        tags,
		SkipTags:    skipTags,
		Concurrency: concurrency,
		ProjectDir:  projectDir,
		ProjectVars: projectCfg.Vars,
		Vars:        vars,
		Phase:       phase,
		Renderer:    renderer,
		Secrets:     secretsResolver,
		StatePath:   stateFilePath(cmd),
	}

	r := runner.New(tgt, chain, cfg)
	return r.Run(ctx, pb)
}
