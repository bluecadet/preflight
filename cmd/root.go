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
	rootCmd.PersistentFlags().StringSliceP("target", "t", nil, "target host(s) or group(s) from inventory")
	rootCmd.PersistentFlags().String("inventory", "", "path to inventory file (default: ./inventory.yml)")
	rootCmd.PersistentFlags().StringArrayP("var", "e", nil, "set a variable (key=value)")
	rootCmd.PersistentFlags().StringSlice("tags", nil, "only run tasks with these tags")
	rootCmd.PersistentFlags().StringSlice("skip-tags", nil, "skip tasks with these tags")
	rootCmd.PersistentFlags().Bool("check", false, "dry-run mode: check what would change without applying")
	rootCmd.PersistentFlags().Bool("diff", false, "show diffs for file changes")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().String("output", "", "output format: text, tui, json, or jsonl (default: tui when interactive, text otherwise)")
	rootCmd.PersistentFlags().Int("concurrency", 0, "max number of targets to operate on in parallel (0 = unlimited)")
	rootCmd.PersistentFlags().String("timeout", "", "overall execution timeout (e.g. 30m, 1h)")
	_ = rootCmd.PersistentFlags().MarkHidden("diff")
}
