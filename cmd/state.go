package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/targeting"
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

func stateFileOverride(cmd *cobra.Command) (string, bool) {
	flag := cmd.Flags().Lookup("state-file")
	if flag == nil || !flag.Changed {
		return "", false
	}
	return stateFilePath(cmd), true
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
	return runStateComparison("state diff", cmd, args)
}

func runStateComparison(label string, cmd *cobra.Command, args []string) error {
	playbookPath := getPlaybookPath(args)
	if err := validateLocalOnlyRunFlags(cmd); err != nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	pb, projectDir, projectCfg, secretsResolver, chain, err := loadPlaybookRunContext(playbookPath)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := fetchPlaybookActionRefs(ctx, pb, chain); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	registry, _, err := buildModuleRegistry(projectDir)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	hosts, err := resolveRunHosts(ctx, cmd, projectDir, registry, secretsResolver)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	overrideStatePath, hasStateOverride := stateFileOverride(cmd)
	if hasStateOverride && len(hosts) > 1 {
		return fmt.Errorf("%s: --state-file can only be used when exactly one host is resolved", label)
	}

	varFlags, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varFlags)

	for idx, host := range hosts {
		statePath := host.StatePath
		if hasStateOverride {
			statePath = overrideStatePath
		}

		state, err := runner.LoadState(statePath)
		if err != nil {
			return fmt.Errorf("%s: load state for %s: %w", label, host.Name, err)
		}

		cfg := runner.Config{
			ProjectVars:    projectCfg.Vars,
			InventoryVars:  host.Vars,
			Vars:           vars,
			TargetVars:     host.TargetVars,
			TargetName:     host.Name,
			Phase:          "plan",
			Secrets:        secretsResolver,
			ModuleRegistry: registry,
		}

		r := runner.New(host.Target, chain, cfg)
		plan, err := r.Plan(ctx, pb)
		if err != nil {
			return fmt.Errorf("%s: plan for %s: %w", label, host.Name, err)
		}

		plannedState, err := r.PlannedTaskState(ctx, plan)
		if err != nil {
			return fmt.Errorf("%s: build planned state for %s: %w", label, host.Name, err)
		}
		comparisons := runner.ComparePlannedTasks(plannedState, state)

		if idx > 0 {
			fmt.Println()
		}
		printStateComparison(host, statePath, plan, state, comparisons)
	}

	return nil
}

func printStateComparison(
	host targeting.ResolvedHost,
	statePath string,
	plan *runner.ExecutionPlan,
	state *runner.State,
	comparisons []runner.TaskComparison,
) {
	fmt.Printf("State diff for playbook: %s\n", plan.PlaybookName)
	fmt.Printf("Target: %s\n", host.Name)
	fmt.Printf("State file: %s\n", statePath)
	fmt.Printf("Last applied: %s\n\n", func() string {
		if state.LastApplied.IsZero() {
			return "(never)"
		}
		return state.LastApplied.UTC().Format("2006-01-02 15:04:05 UTC")
	}())

	fmt.Printf("%-12s %-28s %-16s %s\n", "STATUS", "TASK", "MODULE", "RECORDED STATUS")
	fmt.Printf("%-12s %-28s %-16s %s\n", "------------", "----------------------------", "----------------", "---------------")

	for _, comparison := range comparisons {
		recordedStatus := "(not recorded)"
		if comparison.Status != runner.ComparisonStatusNew {
			recordedStatus = string(comparison.RecordedStatus)
		}
		fmt.Printf("%-12s %-28s %-16s %s\n", comparison.Status, comparison.TaskName, comparison.Module, recordedStatus)
	}
}
