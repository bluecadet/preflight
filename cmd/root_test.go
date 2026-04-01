package cmd

import "testing"

func TestRootCommandSilencesUsageOnErrors(t *testing.T) {
	if !rootCmd.SilenceUsage {
		t.Fatal("expected root command to silence Cobra usage output on errors")
	}
	if !rootCmd.SilenceErrors {
		t.Fatal("expected root command to silence Cobra's built-in error printing")
	}
}
