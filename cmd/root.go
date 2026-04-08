package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "preflight",
	Short:         "Windows-first configuration management CLI for exhibit PCs",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `Preflight is a configuration management CLI for deploying and maintaining
exhibit PCs in museum/gallery environments. It compiles to a single static
binary with no runtime dependencies.`,
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
