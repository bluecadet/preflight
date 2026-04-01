package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

func TestRunPlaybookUsesInventoryTargets(t *testing.T) {
	playbookPath, inventoryPath := writeTestPlaybookWithInventory(t)

	tests := []struct {
		name string
		run  func(*cobra.Command, []string) error
	}{
		{name: "apply", run: runApply},
		{name: "check", run: runCheck},
		{name: "plan", run: runPlan},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newTestCommand()
			if err := cmd.Flags().Set("target", "lab"); err != nil {
				t.Fatalf("Set target: %v", err)
			}
			if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
				t.Fatalf("Set inventory: %v", err)
			}

			var stdout bytes.Buffer
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("Pipe: %v", err)
			}
			os.Stdout = w
			defer func() { os.Stdout = oldStdout }()

			done := make(chan struct{})
			go func() {
				_, _ = stdout.ReadFrom(r)
				close(done)
			}()

			err = tc.run(cmd, []string{playbookPath})
			_ = w.Close()
			<-done
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.name == "plan" {
				out := stdout.String()
				if !strings.Contains(out, "Target: kiosk-a") || !strings.Contains(out, "Target: kiosk-b") {
					t.Fatalf("expected per-target plan output, got %q", out)
				}
			}
		})
	}
}

func TestRunFactsWithInventoryMultipleHostsReturnsMap(t *testing.T) {
	_, inventoryPath := writeTestPlaybookWithInventory(t)
	cmd := newTestCommand()
	if err := cmd.Flags().Set("target", "lab"); err != nil {
		t.Fatalf("Set target: %v", err)
	}
	if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
		t.Fatalf("Set inventory: %v", err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	var stdout bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = stdout.ReadFrom(r)
		close(done)
	}()

	if err := runFacts(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = w.Close()
	<-done

	out := stdout.String()
	if !strings.Contains(out, "\"kiosk-a\"") || !strings.Contains(out, "\"kiosk-b\"") {
		t.Fatalf("expected multi-host facts map, got %q", out)
	}
}

func TestRunPlanAllowsConfiguredConcurrency(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("concurrency", "2"); err != nil {
		t.Fatalf("Set concurrency: %v", err)
	}

	err := runPlan(cmd, []string{writeTestPlaybook(t)})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRunPlanTimeoutReturnsDeadlineExceeded(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("timeout", "0s"); err != nil {
		t.Fatalf("Set timeout: %v", err)
	}

	err := runPlan(cmd, []string{writeTestPlaybook(t)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestRunPlaybookFetchPhaseReturnsNil(t *testing.T) {
	playbookPath := writeTestPlaybook(t)
	cmd := newTestCommand()
	if err := cmd.Flags().Set("phase", "fetch"); err != nil {
		t.Fatalf("Set phase: %v", err)
	}

	if err := runApply(cmd, []string{playbookPath}); err != nil {
		t.Fatalf("expected nil for fetch phase, got %v", err)
	}
}

func TestRunPlaybookStagePhaseWritesBundle(t *testing.T) {
	playbookPath := writeTestPlaybook(t)
	cmd := newTestCommand()
	if err := cmd.Flags().Set("phase", "stage"); err != nil {
		t.Fatalf("Set phase: %v", err)
	}

	if err := runApply(cmd, []string{playbookPath}); err != nil {
		t.Fatalf("expected stage to succeed, got %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(playbookPath), "dist", "bundles", "*.zip"))
	if err != nil {
		t.Fatalf("Glob bundle output: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one staged bundle, got %d", len(matches))
	}
}

func TestRunApplyBundleRoundTrip(t *testing.T) {
	playbookPath := writeTestPlaybook(t)
	stageCmd := newTestCommand()
	if err := stageCmd.Flags().Set("phase", "stage"); err != nil {
		t.Fatalf("Set phase: %v", err)
	}
	if err := runApply(stageCmd, []string{playbookPath}); err != nil {
		t.Fatalf("stage bundle: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(playbookPath), "dist", "bundles", "*.zip"))
	if err != nil {
		t.Fatalf("Glob bundle output: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one staged bundle, got %d", len(matches))
	}

	applyCmd := newTestCommand()
	statePath := filepath.Join(filepath.Dir(playbookPath), "state", "bundle.json")
	if err := applyCmd.Flags().Set("bundle", matches[0]); err != nil {
		t.Fatalf("Set bundle: %v", err)
	}
	if err := applyCmd.Flags().Set("state-file", statePath); err != nil {
		t.Fatalf("Set state-file: %v", err)
	}
	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("apply bundle: %v", err)
	}

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected bundle apply state file, got %v", err)
	}
}

func TestRunPlanDoesNotFetchRemoteActions(t *testing.T) {
	playbookPath := writeRemoteActionPlaybook(t)
	resolver := &testRemoteActionResolver{}

	oldChain := newActionChain
	newActionChain = func(string) action.Chain { return action.Chain{resolver} }
	defer func() { newActionChain = oldChain }()

	cmd := newTestCommand()
	if _, err := captureStdout(t, func() error {
		return runPlan(cmd, []string{playbookPath})
	}); err == nil {
		t.Fatal("expected remote cache miss, got nil")
	}

	if resolver.fetchCalls != 0 {
		t.Fatalf("expected plan to avoid fetching remote actions, got %d fetches", resolver.fetchCalls)
	}
}

func TestRunDiffFetchesRemoteActions(t *testing.T) {
	playbookPath := writeRemoteActionPlaybook(t)
	resolver := &testRemoteActionResolver{}

	oldChain := newActionChain
	newActionChain = func(string) action.Chain { return action.Chain{resolver} }
	defer func() { newActionChain = oldChain }()

	cmd := newTestCommand()
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := cmd.Flags().Set("state-file", statePath); err != nil {
		t.Fatalf("Set state-file: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runDiff(cmd, []string{playbookPath})
	}); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	if resolver.fetchCalls != 1 {
		t.Fatalf("expected one remote fetch, got %d", resolver.fetchCalls)
	}
}

func TestRunDiffUsesResolvedHostContext(t *testing.T) {
	dir := t.TempDir()
	playbookPath := filepath.Join(dir, "playbook.yml")
	inventoryPath := filepath.Join(dir, "inventory.yml")
	statePath := filepath.Join(dir, "state", "kiosk-a.json")

	if err := os.WriteFile(playbookPath, []byte(`
name: host-aware-diff
tasks:
  - name: echo {{ target.name }}
    shell:
      cmd: echo
      args: ["{{ vars.message }}", "{{ target.address }}"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", playbookPath, err)
	}
	if err := os.WriteFile(inventoryPath, []byte(`
groups:
  lab:
    hosts:
      - name: kiosk-a
        address: 10.0.0.1
        transport: local
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", inventoryPath, err)
	}

	hostVars := map[string]any{"message": "hello"}
	targetVars := map[string]any{
		"name":      "kiosk-a",
		"hostname":  "kiosk-a",
		"address":   "10.0.0.1",
		"transport": string(inventory.TransportLocal),
	}

	registry, _, err := buildModuleRegistry(dir)
	if err != nil {
		t.Fatalf("buildModuleRegistry: %v", err)
	}
	pb, err := action.LoadPlaybookFile(playbookPath)
	if err != nil {
		t.Fatalf("LoadPlaybookFile: %v", err)
	}

	hostRunner := runner.New(target.NewLocalTarget(registry), action.Chain{}, runner.Config{
		InventoryVars: hostVars,
		TargetVars:    targetVars,
	})
	plan, err := hostRunner.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plannedState, err := hostRunner.PlannedTaskState(context.Background(), plan)
	if err != nil {
		t.Fatalf("PlannedTaskState: %v", err)
	}

	state := &runner.State{Tasks: make(map[string]runner.TaskSnapshot)}
	for _, task := range plannedState {
		state.RecordTask(runner.TaskSnapshot{
			TaskKey:      task.TaskKey,
			TaskName:     task.TaskName,
			Module:       task.Module,
			DependsOn:    task.DependsOn,
			TaskHash:     task.TaskHash,
			ParamHash:    task.ParamHash,
			ParamSummary: task.ParamSummary,
			Status:       target.StatusOK,
		})
	}
	if err := state.Save(statePath); err != nil {
		t.Fatalf("Save state: %v", err)
	}

	oldResolveHosts := resolveInventoryHosts
	resolveInventoryHosts = func(_ context.Context, inv *inventory.Inventory, selectors []string, registry target.ModuleRegistry, resolver *secrets.Resolver, baseStatePath string) ([]targeting.ResolvedHost, error) {
		if len(selectors) != 1 || selectors[0] != "lab" {
			t.Fatalf("unexpected selectors: %#v", selectors)
		}
		return []targeting.ResolvedHost{{
			Name:       "kiosk-a",
			Vars:       hostVars,
			TargetVars: targetVars,
			StatePath:  statePath,
			Target:     target.NewLocalTarget(registry),
			InventoryRef: inventory.Host{
				Name:      "kiosk-a",
				Address:   "10.0.0.1",
				Transport: inventory.TransportLocal,
			},
		}}, nil
	}
	defer func() { resolveInventoryHosts = oldResolveHosts }()

	cmd := newTestCommand()
	if err := cmd.Flags().Set("target", "lab"); err != nil {
		t.Fatalf("Set target: %v", err)
	}
	if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
		t.Fatalf("Set inventory: %v", err)
	}
	if err := cmd.Flags().Set("state-file", statePath); err != nil {
		t.Fatalf("Set state-file: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runDiff(cmd, []string{playbookPath})
	})
	if err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	if !strings.Contains(out, "Target: kiosk-a") {
		t.Fatalf("expected target section for kiosk-a, got %q", out)
	}
	if !strings.Contains(out, "echo kiosk-a") {
		t.Fatalf("expected rendered target-aware task name, got %q", out)
	}
	if !strings.Contains(out, "UNCHANGED") {
		t.Fatalf("expected unchanged comparison, got %q", out)
	}
}

func TestRunFactsWithInventoryUsesSecretsResolver(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.yml")
	configPath := filepath.Join(dir, "preflight.yml")

	if err := os.WriteFile(inventoryPath, []byte(`
groups:
  lab:
    hosts:
      - name: kiosk-a
        transport: winrm
        username: exhibit
        password_from: secret:winrm-password
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", inventoryPath, err)
	}
	if err := os.WriteFile(configPath, []byte(`
secrets:
  identity: .age/keys.txt
  entries:
    winrm-password:
      file: secrets/winrm-password.age
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", configPath, err)
	}

	oldResolveHosts := resolveInventoryHosts
	resolveInventoryHosts = func(_ context.Context, inv *inventory.Inventory, selectors []string, registry target.ModuleRegistry, resolver *secrets.Resolver, baseStatePath string) ([]targeting.ResolvedHost, error) {
		if resolver == nil || !resolver.HasProviders() {
			t.Fatal("expected facts to pass a configured secrets resolver")
		}
		if len(selectors) != 1 || selectors[0] != "lab" {
			t.Fatalf("unexpected selectors: %#v", selectors)
		}
		return []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, baseStatePath)}, nil
	}
	defer func() { resolveInventoryHosts = oldResolveHosts }()

	cmd := newTestCommand()
	if err := cmd.Flags().Set("target", "lab"); err != nil {
		t.Fatalf("Set target: %v", err)
	}
	if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
		t.Fatalf("Set inventory: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runFacts(cmd, nil)
	}); err != nil {
		t.Fatalf("runFacts: %v", err)
	}
}

func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringSliceP("target", "t", nil, "")
	cmd.Flags().StringArrayP("var", "e", nil, "")
	cmd.Flags().StringSlice("tags", nil, "")
	cmd.Flags().StringSlice("skip-tags", nil, "")
	cmd.Flags().Bool("check", false, "")
	cmd.Flags().Bool("diff", false, "")
	cmd.Flags().BoolP("verbose", "v", false, "")
	cmd.Flags().String("output", "text", "")
	cmd.Flags().Int("concurrency", 0, "")
	cmd.Flags().String("timeout", "", "")
	cmd.Flags().String("phase", "", "")
	cmd.Flags().String("bundle-output-dir", "", "")
	cmd.Flags().String("bundle", "", "")
	cmd.Flags().String("state-file", "", "")
	cmd.Flags().String("inventory", "", "")
	cmd.SetContext(context.Background())
	return cmd
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	var stdout bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = stdout.ReadFrom(r)
		close(done)
	}()

	runErr := fn()
	_ = w.Close()
	<-done
	return stdout.String(), runErr
}

func writeTestPlaybook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(path, []byte(`
name: test
tasks:
  - name: echo
    shell:
      cmd: echo
      args: ["hello"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func writeTestPlaybookWithInventory(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	playbookPath := filepath.Join(dir, "playbook.yml")
	inventoryPath := filepath.Join(dir, "inventory.yml")

	if err := os.WriteFile(playbookPath, []byte(`
name: test
tasks:
  - name: echo
    shell:
      cmd: echo
      args: ["hello"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", playbookPath, err)
	}

	if err := os.WriteFile(inventoryPath, []byte(`
groups:
  lab:
    hosts:
      - name: kiosk-a
        transport: local
      - name: kiosk-b
        transport: local
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", inventoryPath, err)
	}

	return playbookPath, inventoryPath
}

const testRemoteActionRef = "github.com/acme/actions/signage@v1"

type testRemoteActionResolver struct {
	fetched    bool
	fetchCalls int
}

func (r *testRemoteActionResolver) Name() string { return "test-remote" }

func (r *testRemoteActionResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	if ref != testRemoteActionRef {
		return nil, nil
	}
	if !r.fetched {
		return nil, &action.RemoteCacheMissError{Ref: ref}
	}
	return testRemoteAction(), nil
}

func (r *testRemoteActionResolver) Fetch(_ context.Context, ref string) (*action.FetchResult, error) {
	if ref != testRemoteActionRef {
		return nil, nil
	}
	r.fetched = true
	r.fetchCalls++
	sha := "0123456789abcdef0123456789abcdef01234567"
	return &action.FetchResult{
		Entry: action.LockEntry{
			Ref:    ref,
			SHA:    sha,
			Pinned: "github.com/acme/actions/signage@" + sha,
		},
		Action: testRemoteAction(),
	}, nil
}

func testRemoteAction() *action.Action {
	return &action.Action{
		Name: "remote-signage",
		Tasks: []action.Task{
			{
				Name: "remote echo",
				Shell: map[string]any{
					"cmd":  "echo",
					"args": []any{"hello"},
				},
			},
		},
	}
}

func writeRemoteActionPlaybook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(path, []byte(`
name: remote-test
tasks:
  - name: use remote action
    uses: github.com/acme/actions/signage@v1
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}
