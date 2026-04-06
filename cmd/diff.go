package cmd

import (
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <playbook>",
	Short: "Compare the current plan against recorded state",
	Long: `Compare the desired state (from a playbook) against the last recorded state.

This command is a shortcut for: preflight state diff <playbook>

The --state-file flag is inherited from the state parent command.`,
	Args: cobra.ExactArgs(1),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().String("phase", "", "run only up to this phase: plan, fetch, stage, or apply")
	stateCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	return runStateComparison("diff", cmd, args)
}
