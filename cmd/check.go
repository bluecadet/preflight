package cmd

import (
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:     "check <playbook>",
	Short:   "Check what a playbook would change without applying (dry-run)",
	Aliases: []string{"--check"},
	Args:    cobra.ExactArgs(1),
	RunE:    runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	return runPlaybook(cmd, args, true)
}
