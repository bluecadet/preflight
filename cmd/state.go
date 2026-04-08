package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
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

func init() {
	addOutputFlags(stateShowCmd)
	stateShowCmd.Flags().String("state-file", "", "path to state file (default: "+defaultStatePath+")")
	stateCmd.AddCommand(stateShowCmd)
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

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

	renderer.Emit(output.StateEvent{
		StatePath:   path,
		LastApplied: stateLastAppliedString(state),
		Comparisons: stateComparisonsFromState(state),
	})
	return nil
}

func stateLastAppliedString(state *runner.State) string {
	if state.LastApplied.IsZero() {
		return "(never)"
	}
	return state.LastApplied.UTC().Format("2006-01-02 15:04:05 UTC")
}

func stateComparisonsFromState(state *runner.State) []output.StateComparison {
	keys := make([]string, 0, len(state.Tasks))
	for k := range state.Tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	comps := make([]output.StateComparison, 0, len(state.Tasks))
	for _, k := range keys {
		t := state.Tasks[k]
		comps = append(comps, output.StateComparison{
			Status:         "recorded",
			TaskName:       t.TaskName,
			Module:         t.Module,
			RecordedStatus: string(t.Status),
		})
	}
	return comps
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

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

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

	for _, host := range hosts {
		statePath := host.StatePath
		if hasStateOverride {
			statePath = overrideStatePath
		}

		state, err := runner.LoadState(statePath)
		if err != nil {
			return fmt.Errorf("%s: load state for %s: %w", label, host.Name, err)
		}

		cfg := runner.Config{
			ProjectName:    projectCfg.Project,
			ProjectEnv:     projectCfg.Environment,
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

		comps := make([]output.StateComparison, 0, len(comparisons))
		for _, c := range comparisons {
			recordedStatus := "(not recorded)"
			if c.Status != runner.ComparisonStatusNew {
				recordedStatus = string(c.RecordedStatus)
			}
			comps = append(comps, output.StateComparison{
				Status:         string(c.Status),
				TaskName:       c.TaskName,
				Module:         c.Module,
				RecordedStatus: recordedStatus,
			})
		}

		renderer.Emit(output.StateEvent{
			Target:       host.Name,
			PlaybookName: plan.PlaybookName,
			StatePath:    statePath,
			LastApplied:  stateLastAppliedString(state),
			Comparisons:  comps,
		})
	}

	return nil
}
