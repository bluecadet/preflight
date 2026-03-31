package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/claytercek/preflight/internal/action"
	"github.com/claytercek/preflight/internal/module"
	"github.com/claytercek/preflight/internal/runner"
	"github.com/claytercek/preflight/internal/target"
	"github.com/spf13/cobra"
)

const defaultStatePath = "state/provision.json"

var stateCmd = &cobra.Command{
	Use:   "state",
	Short: "Inspect and compare runner state",
}

var stateShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the last applied state from state/provision.json",
	RunE:  runStateShow,
}

var stateDiffCmd = &cobra.Command{
	Use:   "diff <playbook>",
	Short: "Compare desired state (from playbook) vs recorded state",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateDiff,
}

func init() {
	stateCmd.PersistentFlags().String("state-file", "", "path to state file (default: "+defaultStatePath+")")
	stateCmd.AddCommand(stateShowCmd)
	stateCmd.AddCommand(stateDiffCmd)
	rootCmd.AddCommand(stateCmd)
}

func stateFilePath(cmd *cobra.Command) string {
	p, _ := cmd.Flags().GetString("state-file")
	if p == "" {
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, defaultStatePath)
	}
	return p
}

func runStateShow(cmd *cobra.Command, _ []string) error {
	path := stateFilePath(cmd)

	state, err := runner.LoadState(path)
	if err != nil {
		return fmt.Errorf("state show: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(state)
}

func runStateDiff(cmd *cobra.Command, args []string) error {
	playbookPath := getPlaybookPath(args)
	statePath := stateFilePath(cmd)

	// Load recorded state.
	state, err := runner.LoadState(statePath)
	if err != nil {
		return fmt.Errorf("state diff: load state: %w", err)
	}

	// Parse playbook and build a plan to get the desired task list.
	pb, err := action.ParsePlaybookFile(playbookPath)
	if err != nil {
		return fmt.Errorf("state diff: parse playbook: %w", err)
	}

	projectDir, _ := playbookDir(playbookPath)
	chain := action.DefaultChain(projectDir)

	registry := module.Registry()
	tgt := target.NewLocalTarget(registry)

	varFlags, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varFlags)

	cfg := runner.Config{
		Vars:  vars,
		Phase: "plan",
	}

	r := runner.New(tgt, chain, cfg)
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		return fmt.Errorf("state diff: plan: %w", err)
	}

	// Compare plan tasks against recorded state.
	fmt.Printf("State diff for playbook: %s\n", plan.PlaybookName)
	fmt.Printf("Last applied: %s\n\n", func() string {
		if state.LastApplied.IsZero() {
			return "(never)"
		}
		return state.LastApplied.Format("2006-01-02 15:04:05 UTC")
	}())

	fmt.Printf("%-10s %-40s %s\n", "STATUS", "TASK", "RECORDED STATUS")
	fmt.Printf("%-10s %-40s %s\n", "----------", "----------------------------------------", "---------------")

	for _, pt := range plan.Tasks {
		recorded, ok := state.Results[pt.ID]
		if !ok {
			fmt.Printf("%-10s %-40s %s\n", "NEW", pt.Name, "(not recorded)")
		} else {
			fmt.Printf("%-10s %-40s %s\n", "KNOWN", pt.Name, string(recorded.Status))
		}
	}

	// Report tasks in state but not in the plan (removed tasks).
	planIDs := make(map[string]bool)
	for _, pt := range plan.Tasks {
		planIDs[pt.ID] = true
	}
	for id, result := range state.Results {
		if !planIDs[id] {
			fmt.Printf("%-10s %-40s %s\n", "REMOVED", result.TaskName, string(result.Status))
		}
	}

	return nil
}
