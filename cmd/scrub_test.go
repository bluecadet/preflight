package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

// echoSecretTarget emits the resolved PASSWORD env param as task output, the
// way a script that prints its environment would.
type echoSecretTarget struct{}

func (echoSecretTarget) Execute(_ context.Context, taskID, _ string, params map[string]any, _ target.ExecutionOptions, _ bool, onOutput target.OutputFunc) (target.Result, error) {
	env, _ := params["env"].(map[string]any)
	line := "password is " + env["PASSWORD"].(string)
	if onOutput != nil {
		onOutput(line)
	}
	return target.Result{TaskID: taskID, Status: target.StatusChanged, Output: []string{line}}, nil
}

func (echoSecretTarget) Info(context.Context) (target.TargetInfo, error) {
	return target.TargetInfo{OSFamily: target.OSFamilyWindows, RuntimeKind: target.RuntimeKindWindowsPowerShell, Transport: target.TransportSSH}, nil
}

func (echoSecretTarget) Transport() target.Transport { return target.TransportSSH }

func TestRunApplyScrubsResolvedSecretsFromAllSinks(t *testing.T) {
	const secret = "hunter2"

	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			playbookPath, _ := writeSecretBundleProject(t, false)
			dir := filepath.Dir(playbookPath)
			cfgPath := filepath.Join(dir, "preflight.yml")
			cfg, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg = append(cfg, []byte("inventory:\n  hosts:\n    - name: box\n      transport: ssh\n")...)
			if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
				t.Fatal(err)
			}

			old := resolveInventoryHosts
			resolveInventoryHosts = func(context.Context, *inventory.Inventory, []string, target.ModuleRegistry, *secrets.Resolver, string) ([]targeting.ResolvedHost, error) {
				return []targeting.ResolvedHost{{Name: "box", Vars: map[string]any{}, TargetVars: map[string]any{}, Target: echoSecretTarget{}}}, nil
			}
			defer func() { resolveInventoryHosts = old }()

			cmd := newTestCommand()
			for flag, val := range map[string]string{"output": format, "run-id": "scrub"} {
				if err := cmd.Flags().Set(flag, val); err != nil {
					t.Fatalf("Set %s: %v", flag, err)
				}
			}

			out, err := captureStdout(t, func() error { return runApply(cmd, []string{playbookPath}) })
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if strings.Contains(out, secret) {
				t.Errorf("%s output leaked secret:\n%s", format, out)
			}
			if !strings.Contains(out, "password is ***") {
				t.Errorf("%s output missing scrubbed line:\n%s", format, out)
			}

			runLog, err := os.ReadFile(filepath.Join(dir, ".preflight", "runs", "scrub", "run.jsonl"))
			if err != nil {
				t.Fatalf("read run.jsonl: %v", err)
			}
			if strings.Contains(string(runLog), secret) {
				t.Errorf("run.jsonl leaked secret:\n%s", runLog)
			}
			if !strings.Contains(string(runLog), "password is ***") {
				t.Errorf("run.jsonl missing scrubbed line:\n%s", runLog)
			}
		})
	}
}
