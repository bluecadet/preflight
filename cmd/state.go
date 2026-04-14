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

func stateFilePath(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("state-file")
	if p == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory for state file: %w", err)
		}
		return filepath.Join(cwd, defaultStatePath), nil
	}
	return p, nil
}

func stateFileOverride(cmd *cobra.Command) (string, bool, error) {
	flag := cmd.Flags().Lookup("state-file")
	if flag == nil || !flag.Changed {
		return "", false, nil
	}
	path, err := stateFilePath(cmd)
	if err != nil {
		return "", false, fmt.Errorf("resolve state file override: %w", err)
	}
	return path, true, nil
}

func runStateShow(cmd *cobra.Command, _ []string) error {
	path, err := stateFilePath(cmd)
	if err != nil {
		return fmt.Errorf("state show: %w", err)
	}

	state, err := runner.LoadState(path)
	if err != nil {
		return fmt.Errorf("state show: %w", err)
	}

	renderer := newRenderer(cmd)
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

	renderer := newRenderer(cmd)
	defer renderer.Close()

	session, err := newPlaybookSession(ctx, playbookPath, true)
	if err != nil {
		return wrapLabelError(label, err)
	}
	hosts, err := resolveRunHosts(ctx, cmd, session.ProjectDir, session.Registry, session.Secrets)
	if err != nil {
		return wrapLabelError(label, err)
	}

	overrideStatePath, hasStateOverride, err := stateFileOverride(cmd)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
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
			ProjectName:    session.ProjectCfg.Project,
			ProjectEnv:     session.ProjectCfg.Environment,
			ProjectVars:    session.ProjectCfg.Vars,
			InventoryVars:  host.Vars,
			Vars:           vars,
			TargetVars:     host.TargetVars,
			TargetName:     host.Name,
			Phase:          "plan",
			Secrets:        session.Secrets,
			ModuleRegistry: session.Registry,
		}

		r := runner.New(host.Target, session.Chain, cfg)
		plan, err := r.Plan(ctx, session.Playbook)
		if err != nil {
			return wrapHostLabelError(label, "plan", host.Name, err)
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
