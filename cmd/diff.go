package cmd

import "github.com/spf13/cobra"

func init() {
	stateCmd.AddCommand(newDiffCommand(false))
	rootCmd.AddCommand(newDiffCommand(true))
}

func runDiff(cmd *cobra.Command, args []string) error {
	return runStateComparison("diff", cmd, args)
}

func newDiffCommand(rootAlias bool) *cobra.Command {
	longText := `Compare the desired state (from a playbook) against the last recorded state.`
	if rootAlias {
		longText += `

This command is a shortcut for: preflight state diff <playbook>.`
	}
	cmd := &cobra.Command{
		Use:   "diff <playbook>",
		Short: "Compare the current plan against recorded state",
		Long:  longText,
		Args:  cobra.ExactArgs(1),
		RunE:  runDiff,
	}
	cmd.Flags().String("state-file", "", "path to state file (default: "+defaultStatePath+")")
	return cmd
}
