package cmd

import (
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check <playbook>",
	Short: "Check what a playbook would change without applying (dry-run)",
	Long: `Check resolves and evaluates a playbook in dry-run mode without applying any
changes. No modules will be applied; Check() is called for each task and the
result is reported.

This command is equivalent to: preflight apply --check <playbook>`,
	Args: cobra.ExactArgs(1),
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	return runPlaybook(cmd, args, true)
}
