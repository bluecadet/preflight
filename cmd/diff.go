package cmd

import (
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <playbook>",
	Short: "Compare the current plan against recorded state",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiff,
}

func init() {
	diffCmd.Flags().String("state-file", "", "path to state file (default: "+defaultStatePath+")")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	return runStateComparison("diff", cmd, args)
}
