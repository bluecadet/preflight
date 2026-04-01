package cmd

import "github.com/spf13/cobra"

var stageCmd = &cobra.Command{
	Use:   "stage <playbook>",
	Short: "Assemble staged offline bundles for a playbook",
	Args:  cobra.ExactArgs(1),
	RunE:  runStage,
}

func init() {
	stageCmd.Flags().String("bundle-output-dir", "", "directory for staged bundle zips")
	stageCmd.Flags().Bool("allow-plaintext-secrets-in-bundle", false, "allow staging bundles that contain plaintext secrets")
	rootCmd.AddCommand(stageCmd)
}

func runStage(cmd *cobra.Command, args []string) error {
	if err := cmd.Flags().Set("phase", "stage"); err != nil {
		return err
	}
	return runPlaybook(cmd, args, false)
}
