package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/logging"
)

var rootCmd = &cobra.Command{
	Use:           "preflight",
	Short:         "Windows-first configuration management CLI for managed endpoints",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `Preflight is a configuration management CLI for deploying and maintaining
Windows endpoints such as kiosks, signage, and other dedicated systems. It
compiles to a single static binary with no runtime dependencies.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		verbose := false
		if flag := cmd.Flags().Lookup("verbose"); flag != nil {
			verbose, _ = cmd.Flags().GetBool("verbose")
		}
		logging.Init(verbose)
		return nil
	},
}

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(version, commit, date string) {
	buildVersion = version
	buildCommit = commit
	buildDate = date
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
}
