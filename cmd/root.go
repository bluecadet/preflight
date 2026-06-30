package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/winutil"
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
		initLogger(verbose)
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
	winutil.RefreshProcessPath()
	buildVersion = version
	buildCommit = commit
	buildDate = date
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// initLogger configures the default slog logger. When verbose is true, the level is
// set to Debug; otherwise it is Warn (suppressing Info-level noise during
// normal operation).
func initLogger(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

func init() {
	rootCmd.AddCommand(moduleExecCmd)
}
