package cmd

import (
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <playbook>",
	Short: "Show what would change if the playbook were applied (read-only check mode)",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiff,
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	// diff is check mode + the --diff flag already set by root; force dry-run.
	return runPlaybook(cmd, args, true)
}
