package cmd

import "github.com/spf13/cobra"

func init() {
	stateCmd.AddCommand(newDiffCommand())
}

func runDiff(cmd *cobra.Command, args []string) error {
	return runStateComparison("diff", cmd, args)
}

func newDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <playbook>",
		Short: "Compare the current plan against recorded state",
		Long:  `Compare the desired state (from a playbook) against the last recorded state.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runDiff,
	}
	addTargetingFlags(cmd)
	addVarFlags(cmd)
	addOutputFlags(cmd)
	addTimeoutFlag(cmd)
	cmd.Flags().String("state-file", "", "path to state file (default: "+defaultStatePath+")")
	return cmd
}
