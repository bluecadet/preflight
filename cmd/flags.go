package cmd

import "github.com/spf13/cobra"

func addTargetingFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceP("target", "t", nil, "target host(s) or group(s) from inventory (default: all inventory hosts when available)")
}

func addVarFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayP("var", "e", nil, "set a variable (key=value)")
}

func addTagFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("tags", nil, "only run tasks with these tags")
	cmd.Flags().StringSlice("skip-tags", nil, "skip tasks with these tags")
}

func addOutputFlags(cmd *cobra.Command) {
	cmd.Flags().BoolP("verbose", "v", false, "verbose output")
	cmd.Flags().String("output", "", "output format: text, tui, or json (default: tui when interactive, text otherwise)")
	cmd.Flags().Int("max-fail-lines", 80, "max lines of output to show for a failed task (0 = unlimited)")
}

func addConcurrencyFlag(cmd *cobra.Command) {
	cmd.Flags().Int("concurrency", 0, "max number of targets to operate on in parallel (0 = unlimited)")
}

func addTimeoutFlag(cmd *cobra.Command) {
	cmd.Flags().String("timeout", "", "overall execution timeout (e.g. 30m, 1h)")
}
