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

func TestApplyCommandSupportsStateFileFlag(t *testing.T) {
	if applyCmd.Flags().Lookup("state-file") == nil {
		t.Fatal("expected apply command to define --state-file")
	}
}

func TestStateDiffCommandSupportsStateFileFlag(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"state", "diff"})
	if err != nil {
		t.Fatalf("Find(state diff): %v", err)
	}
	if cmd == nil {
		t.Fatal("expected to find state diff command")
	}
	if cmd.Flags().Lookup("state-file") == nil {
		t.Fatal("expected state diff command to define --state-file")
	}
}
