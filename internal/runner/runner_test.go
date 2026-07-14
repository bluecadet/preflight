package runner

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/bundle"
	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/stdlib"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
	"github.com/bluecadet/preflight/internal/template"
)

type mockTarget struct {
	results   []target.Result // in order; last result is reused if list is exhausted
	calls     []mockCall
	output    []string
	execErr   error
	transport target.Transport
}

type closableMockTarget struct {
	mockTarget
	closeCalls int
}

type mockCall struct {
	TaskID string
	Module string
	DryRun bool
	Params map[string]any
}

func assertContainsInOrder(t *testing.T, text string, fragments ...string) {
	t.Helper()
	offset := 0
	for _, fragment := range fragments {
		idx := strings.Index(text[offset:], fragment)
		if idx < 0 {
			t.Fatalf("expected text to contain %q after offset %d, got:\n%s", fragment, offset, text)
		}
		offset += idx + len(fragment)
	}
}

func (m *mockTarget) Execute(_ context.Context, taskID, module string, params map[string]any, _ target.ExecutionOptions, dryRun bool, onOutput target.OutputFunc) (target.Result, error) {
	var copied map[string]any
	if params != nil {
		copied = make(map[string]any, len(params))
		maps.Copy(copied, params)
	}
	m.calls = append(m.calls, mockCall{TaskID: taskID, Module: module, DryRun: dryRun, Params: copied})
	if onOutput != nil {
		for _, line := range m.output {
			onOutput(line)
		}
	}
	if m.execErr != nil {
		return target.Result{TaskID: taskID, Output: append([]string(nil), m.output...)}, m.execErr
	}
	if len(m.results) == 0 {
		return target.Result{TaskID: taskID, Status: target.StatusOK, Output: append([]string(nil), m.output...)}, nil
	}
	idx := len(m.calls) - 1
	if idx >= len(m.results) {
		idx = len(m.results) - 1
	}
	r := m.results[idx]
	r.TaskID = taskID
	if len(m.output) > 0 && len(r.Output) == 0 {
		r.Output = append([]string(nil), m.output...)
	}
	return r, nil
}

func (m *mockTarget) Info(_ context.Context) (target.TargetInfo, error) {
	return target.TargetInfo{Transport: m.Transport()}, nil
}

func (m *mockTarget) Transport() target.Transport {
	if m.transport != "" {
		return m.transport
	}
	return target.TransportSSH
}

func (m *mockTarget) RunPowerShell(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *closableMockTarget) Close() error {
	m.closeCalls++
	return nil
}

type recordingRenderer struct {
	events []output.Event
}

func (r *recordingRenderer) Emit(e output.Event) { r.events = append(r.events, e) }
func (r *recordingRenderer) Close()              {}

type emptyChain struct{}

func (emptyChain) Resolve(_ context.Context, ref string) (*action.Action, error) { return nil, nil }
func (emptyChain) Name() string                                                  { return "empty" }

func emptyResolver() action.Chain {
	return action.Chain{emptyChain{}}
}

func newShellPlaybook(name string) *action.Playbook {
	return &action.Playbook{
		Name: name,
		Tasks: []action.Task{
			{
				Name:          "run echo",
				InlineModules: map[string]map[string]any{"shell": {"cmd": "echo hello"}},
			},
		},
	}
}

func ageGenerateIdentity(dir string) (*age.X25519Identity, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "keys.txt"), []byte(identity.String()+"\n"), 0o600); err != nil {
		return nil, err
	}
	return identity, nil
}

func TestPlanSingleTask(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{})
	pb := newShellPlaybook("test play")

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if plan == nil {
		t.Fatal("Plan returned nil plan")
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Module != "shell" {
		t.Errorf("expected module %q, got %q", "shell", plan.Tasks[0].Module)
	}
	if plan.Tasks[0].ID == "" {
		t.Fatal("expected stable task ID to be populated")
	}
	if strings.Contains(plan.Tasks[0].ID, "task-0") {
		t.Fatalf("expected non-positional task ID, got %q", plan.Tasks[0].ID)
	}
}

func TestRunClosesTarget(t *testing.T) {
	mt := &closableMockTarget{
		mockTarget: mockTarget{
			results: []target.Result{{Status: target.StatusOK}},
		},
	}
	r := New(mt, emptyResolver(), Config{})

	if err := r.Run(context.Background(), newShellPlaybook("close target")); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if mt.closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", mt.closeCalls)
	}
}

func TestPlanMergesProjectVarsAndActionInputs(t *testing.T) {
	resolver := action.Chain{&staticResolver{
		action: &action.Action{
			Name: "preflight/autologin",
			Inputs: map[string]action.Input{
				"username": {Required: true},
				"password": {},
			},
			Tasks: []action.Task{
				{
					Name: "configure autologin",
					InlineModules: map[string]map[string]any{"registry": {
						"path": "HKLM:\\Software\\Winlogon",
						"values": map[string]any{
							"DefaultUserName": "{{ vars.username }}",
							"DefaultPassword": "{{ vars.password }}",
							"Site":            "{{ vars.site }}",
						},
					}},
				},
			},
		},
	}}
	r := New(&mockTarget{}, resolver, Config{
		ProjectVars: map[string]any{"site": "Lobby"},
	})
	pb := &action.Playbook{
		Name: "test",
		Tasks: []action.Task{
			{
				Name: "autologin",
				Uses: "preflight/autologin",
				With: map[string]any{
					"username": "kiosk",
					"password": "secret:autologin-password",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	preview, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask returned error: %v", err)
	}
	values, ok := preview.Params["values"].(map[string]any)
	if !ok {
		t.Fatalf("expected values map, got %T", preview.Params["values"])
	}
	if values["DefaultPassword"] != "secret:autologin-password" {
		t.Fatalf("expected secret ref to be preserved in plan, got %#v", values["DefaultPassword"])
	}
	if values["Site"] != "Lobby" {
		t.Fatalf("expected project var to flow into action rendering, got %#v", values["Site"])
	}
}

func TestPlanMergesBecomeDefaultsAcrossActionExpansion(t *testing.T) {
	resolver := action.Chain{&staticResolver{
		action: &action.Action{
			Name: "acme/demo",
			Defaults: action.TaskDefaults{
				Become: map[string]any{"method": "sudo"},
			},
			Tasks: []action.Task{
				{
					Name:          "echo",
					InlineModules: map[string]map[string]any{"shell": {"cmd": "echo", "args": []any{"hello"}}},
				},
			},
		},
	}}
	r := New(&mockTarget{}, resolver, Config{})
	pb := &action.Playbook{
		Name: "test",
		Defaults: action.TaskDefaults{
			Become: map[string]any{"user": "playbook-user"},
		},
		Tasks: []action.Task{
			{
				Name:   "call action",
				Uses:   "acme/demo",
				Become: map[string]any{"user": "task-user"},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if got := plan.Tasks[0].Become["user"]; got != "task-user" {
		t.Fatalf("expected task become user override, got %#v", got)
	}
	if got := plan.Tasks[0].Become["method"]; got != "sudo" {
		t.Fatalf("expected action default method, got %#v", got)
	}
	if got := plan.Tasks[0].Become["enabled"]; got != true {
		t.Fatalf("expected become enabled by default, got %#v", got)
	}
}

func TestPlanTaskBecomeCanDisableInheritedDefaults(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{})
	pb := &action.Playbook{
		Name: "test",
		Defaults: action.TaskDefaults{
			Become: map[string]any{"user": "playbook-user"},
		},
		Tasks: []action.Task{
			{
				Name:          "echo",
				Become:        map[string]any{"enabled": false},
				InlineModules: map[string]map[string]any{"shell": {"cmd": "echo", "args": []any{"hello"}}},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if got := plan.Tasks[0].Become["enabled"]; got != false {
		t.Fatalf("expected become disabled override, got %#v", got)
	}
	if _, ok := plan.Tasks[0].Become["user"]; ok {
		t.Fatalf("expected disabled override to clear inherited become fields, got %#v", plan.Tasks[0].Become)
	}
	if len(plan.Tasks[0].Become) != 1 {
		t.Fatalf("expected disabled override to keep only enabled=false, got %#v", plan.Tasks[0].Become)
	}
}

func TestApplyPassesRenderedExecutionOptionsToTarget(t *testing.T) {
	tgt := &targettest.Fake{
		InfoValue: target.TargetInfo{
			Hostname:  "remote-linux",
			OSFamily:  target.OSFamilyLinux,
			Transport: target.TransportSSH,
		},
		Results: []target.Result{{Status: target.StatusChanged}},
	}
	r := New(tgt, emptyResolver(), Config{
		TargetVars: map[string]any{"run_as": "appuser"},
	})
	plan := &ExecutionPlan{
		PlaybookName: "become",
		Tasks: []*PlanTask{
			{
				ID:     "task-0",
				Name:   "echo",
				Ref:    "echo",
				Module: "shell",
				Scope:  template.NewScope(),
				Params: map[string]any{"cmd": "echo"},
				Become: map[string]any{
					"user":   "{{ target.run_as }}",
					"method": "sudo",
				},
			},
		},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(tgt.Calls) != 1 {
		t.Fatalf("expected one target call, got %d", len(tgt.Calls))
	}
	got := tgt.Calls[0].Options.Become
	if got == nil {
		t.Fatal("expected become options to be passed to target")
	}
	if !got.Enabled || got.User != "appuser" || got.Method != "sudo" {
		t.Fatalf("unexpected become options: %#v", got)
	}
}

func TestDAGDependsOnOrder(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "task-a"}
	taskB := &PlanTask{ID: "task-1", Name: "task-b", DependsOn: []string{"task-a"}}

	dag, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err != nil {
		t.Fatalf("BuildDAG error: %v", err)
	}

	ordered := dag.TopologicalOrder()
	if len(ordered) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(ordered))
	}
	if ordered[0].ID != "task-0" || ordered[1].ID != "task-1" {
		t.Errorf("wrong order: got %v, want [task-0, task-1]", []string{ordered[0].ID, ordered[1].ID})
	}
}

func TestDAGCycleDetection(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "task-a", DependsOn: []string{"task-b"}}
	taskB := &PlanTask{ID: "task-1", Name: "task-b", DependsOn: []string{"task-a"}}

	_, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestPlanStdlibWindowsMachineRendersLeafInputs(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{
		ProjectVars: map[string]any{
			"device_name": "Gallery-Kiosk-01",
			"device_tz":   "Eastern Standard Time",
		},
	})
	pb := &action.Playbook{
		Name: "windows-machine",
		Tasks: []action.Task{
			{
				Name: "machine baseline",
				Uses: "preflight/windows-machine",
				With: map[string]any{
					"computer_name": "{{ vars.device_name }}",
					"timezone":      "{{ vars.device_tz }}",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 6 {
		t.Fatalf("expected 6 expanded tasks, got %d", len(plan.Tasks))
	}

	previewName, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask(computer name) returned error: %v", err)
	}
	checkScript, ok := previewName.Params["check_script"].(string)
	if !ok {
		t.Fatalf("expected check_script string, got %T", previewName.Params["check_script"])
	}
	if !strings.Contains(checkScript, "Gallery-Kiosk-01") {
		t.Fatalf("expected rendered computer name in check script, got:\n%s", checkScript)
	}

	previewTZ, err := PreviewTask(plan.Tasks[1], nil)
	if err != nil {
		t.Fatalf("PreviewTask(timezone) returned error: %v", err)
	}
	tzScript, ok := previewTZ.Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", previewTZ.Params["script"])
	}
	if !strings.Contains(tzScript, "Eastern Standard Time") {
		t.Fatalf("expected rendered timezone in script, got:\n%s", tzScript)
	}
}

func TestPlanStdlibWindowsPowerRendersTemplatedSettingsLists(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{})
	pb := &action.Playbook{
		Name: "windows-power",
		Tasks: []action.Task{
			{
				Name: "power baseline",
				Uses: "preflight/windows-power",
				With: map[string]any{
					"plan_name":          "Exhibit Plan",
					"display_timeout_ac": 5,
					"display_timeout_dc": 7,
					"sleep_timeout_ac":   0,
					"sleep_timeout_dc":   10,
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected 3 expanded tasks, got %d", len(plan.Tasks))
	}

	previewPlan, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask(power plan) returned error: %v", err)
	}
	if previewPlan.Module != "power_plan" {
		t.Fatalf("expected power_plan task, got %q", previewPlan.Module)
	}
	settings, ok := previewPlan.Params["settings"].([]any)
	if !ok {
		t.Fatalf("expected settings list, got %T", previewPlan.Params["settings"])
	}
	if len(settings) != 2 {
		t.Fatalf("expected 2 settings, got %d", len(settings))
	}
	first, ok := settings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first setting map, got %T", settings[0])
	}
	if first["ac_value"] != "5" || first["dc_value"] != "7" {
		t.Fatalf("unexpected first setting values: %#v", first)
	}
}

func TestPlanStdlibWindowsShellCarriesUserToUserScopedTasks(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{
		ProjectVars: map[string]any{"kiosk_user": "kiosk"},
	})
	pb := &action.Playbook{
		Name: "windows-shell",
		Tasks: []action.Task{
			{
				Name: "shell defaults",
				Uses: "preflight/windows-shell",
				With: map[string]any{
					"user":                     "{{ vars.kiosk_user }}",
					"theme_mode":               "dark",
					"transparency_effects":     false,
					"taskbar_auto_hide":        true,
					"clear_desktop_background": true,
					"clear_desktop_shortcuts":  true,
					"hide_recycle_bin":         true,
					"show_hidden_files":        true,
					"show_file_extensions":     true,
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	byName := make(map[string]*PlanTask, len(plan.Tasks))
	for _, task := range plan.Tasks {
		preview, err := PreviewTask(task, nil)
		if err != nil {
			t.Fatalf("PreviewTask(%s) returned error: %v", task.Name, err)
		}
		byName[preview.Name] = preview
	}

	registryTaskNames := []string{
		"Clear desktop background",
		"Configure theme mode",
		"Configure transparency effects",
		"Configure hidden files visibility",
		"Configure file extensions visibility",
		"Configure Recycle Bin desktop icon visibility (NewStartPanel)",
		"Configure Recycle Bin desktop icon visibility (ClassicStartMenu)",
		"Configure common desktop icon visibility (NewStartPanel)",
		"Configure common desktop icon visibility (ClassicStartMenu)",
	}
	for _, name := range registryTaskNames {
		task := byName[name]
		if task == nil {
			t.Fatalf("missing expanded task %q", name)
		}
		if task.Module != "registry" {
			t.Fatalf("%s: expected registry module, got %q", name, task.Module)
		}
		if task.Params["user"] != "kiosk" {
			t.Fatalf("%s: expected registry user kiosk, got %#v", name, task.Params["user"])
		}
	}

	taskbar := byName["Configure taskbar auto-hide"]
	if taskbar == nil {
		t.Fatal("missing taskbar auto-hide task")
	}
	if taskbar.Module != "registry" {
		t.Fatalf("taskbar auto-hide should use registry module, got %q", taskbar.Module)
	}
	if taskbar.Params["user"] != "kiosk" {
		t.Fatalf("expected taskbar registry user kiosk, got %#v", taskbar.Params["user"])
	}
	values, ok := taskbar.Params["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("expected taskbar registry value list, got %#v", taskbar.Params["values"])
	}
	settings, ok := values[0].(map[string]any)
	if !ok {
		t.Fatalf("expected taskbar value spec, got %T", values[0])
	}
	if settings["name"] != "Settings" || settings["type"] != "binary" {
		t.Fatalf("unexpected taskbar value spec: %#v", settings)
	}
	patch, ok := settings["patch"].([]any)
	if !ok || len(patch) != 1 {
		t.Fatalf("expected taskbar binary patch, got %#v", settings["patch"])
	}
	bytePatch, ok := patch[0].(map[string]any)
	if !ok {
		t.Fatalf("expected taskbar patch spec, got %T", patch[0])
	}
	if bytePatch["offset"] != 8 || bytePatch["data"] != "3" {
		t.Fatalf("expected taskbar auto-hide patch offset 8 data 3, got %#v", bytePatch)
	}

	shortcuts := byName["Clear desktop shortcuts"]
	if shortcuts == nil {
		t.Fatal("missing clear desktop shortcuts task")
	}
	shortcutEnv, ok := shortcuts.Params["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected shortcuts env map, got %T", shortcuts.Params["env"])
	}
	if shortcutEnv["PREFLIGHT_USER"] != "kiosk" {
		t.Fatalf("expected shortcuts user env kiosk, got %#v", shortcutEnv["PREFLIGHT_USER"])
	}
	if !strings.Contains(shortcuts.Params["script"].(string), "ProfileList") {
		t.Fatal("expected shortcuts script to resolve the target user's desktop path")
	}
	for _, fragment := range []string{"User Shell Folders", "Common Desktop", "OneDrive*", ".website"} {
		if !strings.Contains(shortcuts.Params["script"].(string), fragment) {
			t.Fatalf("expected shortcuts script to contain %q", fragment)
		}
	}
}

func TestPlanStdlibGitSyncRendersComprehensiveInputs(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{
		ProjectVars: map[string]any{
			"git_token": "secret:github-token",
			"ssh_key":   "secret:github-deploy-key",
		},
	})
	pb := &action.Playbook{
		Name: "git-sync",
		Tasks: []action.Task{
			{
				Name: "sync content",
				Uses: "preflight/git-sync",
				With: map[string]any{
					"repo":                         "git@github.com:example/private-repo.git",
					"dest":                         "C:\\Exhibits\\App",
					"ref":                          "main",
					"remote":                       "upstream",
					"local_branch":                 "deploy",
					"depth":                        5,
					"fetch":                        true,
					"prune":                        true,
					"fetch_tags":                   false,
					"reset":                        true,
					"clean":                        true,
					"clean_ignored":                true,
					"submodules":                   true,
					"lfs":                          true,
					"safe_directory":               true,
					"http_password":                "{{ vars.git_token }}",
					"ssh_private_key":              "{{ vars.ssh_key }}",
					"ssh_strict_host_key_checking": false,
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 expanded tasks, got %d", len(plan.Tasks))
	}

	trustPreview, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask(git trust) returned error: %v", err)
	}
	if trustPreview.Name != "Trust Git repository directory" {
		t.Fatalf("expected trust task first, got %q", trustPreview.Name)
	}
	if trustPreview.When != "true" {
		t.Fatalf("expected trust task to be gated by safe_directory, got %q", trustPreview.When)
	}
	trustScript, ok := trustPreview.Params["script"].(string)
	if !ok {
		t.Fatalf("expected trust script string, got %T", trustPreview.Params["script"])
	}
	for _, want := range []string{
		"Convert-ToGitSafeDirectory",
		"config', '--global', '--add', 'safe.directory', $safePath",
	} {
		if !strings.Contains(trustScript, want) {
			t.Fatalf("expected git trust script to contain %q, got:\n%s", want, trustScript)
		}
	}

	preview, err := PreviewTask(plan.Tasks[1], nil)
	if err != nil {
		t.Fatalf("PreviewTask(git sync) returned error: %v", err)
	}
	if preview.Module != "powershell" {
		t.Fatalf("expected powershell task, got %q", preview.Module)
	}

	env, ok := preview.Params["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %T", preview.Params["env"])
	}
	if env["PREFLIGHT_GIT_HTTP_PASSWORD"] != "secret:github-token" {
		t.Fatalf("expected HTTP password to remain a secret ref, got %#v", env["PREFLIGHT_GIT_HTTP_PASSWORD"])
	}
	if env["PREFLIGHT_GIT_SSH_PRIVATE_KEY"] != "secret:github-deploy-key" {
		t.Fatalf("expected SSH key to remain a secret ref, got %#v", env["PREFLIGHT_GIT_SSH_PRIVATE_KEY"])
	}

	script, ok := preview.Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", preview.Params["script"])
	}
	for _, want := range []string{
		"git@github.com:example/private-repo.git",
		"C:\\Exhibits\\App",
		"$depth = [int]'5'",
		"$remote = @'",
		"upstream",
		"GIT_ASKPASS",
		"GIT_SSH_COMMAND",
		"safe.directory",
		"submodule', 'update', '--init', '--recursive",
		"lfs', 'pull",
		"clean', $cleanMode",
		"checkout', '-B', $localBranch",
		"--no-tags",
		"Add-GitGlobalConfig 'credential.helper' ''",
		"UTF8Encoding",
		"System.Collections.Generic.List[string]",
		"$gitGlobalArgs.Add('-c')",
		"$gitGlobalArgs.Add(\"$Key=$Value\")",
		"Resolve-GitCommit $ref $fetch",
		"reset', '--hard', $targetRef",
		"function Clear-EnvVar",
		"Clear-EnvVar 'GCM_INTERACTIVE'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected git-sync script to contain %q, got:\n%s", want, script)
		}
	}
	assertContainsInOrder(t, script,
		"$candidates = @()",
		"$candidates += \"refs/remotes/$remote/$Name\"",
		"$candidates += \"$remote/$Name\"",
		"$candidates += $Name",
	)
	if strings.Contains(script, "Set-Content -LiteralPath $path -Value $Content -NoNewline -Encoding UTF8") {
		t.Fatalf("expected git-sync temp files to avoid UTF-8 BOMs, got:\n%s", script)
	}
	if strings.Contains(script, "$resetRef = Resolve-GitCommit") {
		t.Fatalf("expected git-sync reset to use the fetched target ref, got:\n%s", script)
	}
	if strings.Contains(script, "credential.interactive=false") {
		t.Fatalf("expected git-sync script to keep GIT_ASKPASS available, got:\n%s", script)
	}
	if strings.Contains(script, "secret:github-token") || strings.Contains(script, "secret:github-deploy-key") {
		t.Fatalf("expected git-sync script to avoid embedding secret refs, got:\n%s", script)
	}
	if strings.Contains(script, "Remove-Item Env:\\GCM_INTERACTIVE") {
		t.Fatalf("expected git-sync cleanup to avoid unguarded env removal, got:\n%s", script)
	}
	if strings.Contains(script, "$script:") {
		t.Fatalf("expected git-sync script to avoid script-scope variables under persistent PowerShell, got:\n%s", script)
	}
	if strings.Contains(script, "Set-Variable -Name gitGlobalArgs") {
		t.Fatalf("expected git-sync script to avoid scope-sensitive gitGlobalArgs mutation, got:\n%s", script)
	}
}

func TestPlanStdlibGitSyncEnablesSafeDirectoryByDefault(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	r := New(&mockTarget{}, resolver, Config{})
	pb := &action.Playbook{
		Name: "git-sync",
		Tasks: []action.Task{
			{
				Name: "sync content",
				Uses: "preflight/git-sync",
				With: map[string]any{
					"repo": "https://github.com/example/content.git",
					"dest": "C:\\bluecadet\\phillies-rings-touchscreen",
					"ref":  "main",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 expanded tasks, got %d", len(plan.Tasks))
	}
	trustPreview, err := PreviewTask(plan.Tasks[0], nil)
	if err != nil {
		t.Fatalf("PreviewTask(git trust) returned error: %v", err)
	}
	trustCheck, ok := trustPreview.Params["check_script"].(string)
	if !ok {
		t.Fatalf("expected trust check_script string, got %T", trustPreview.Params["check_script"])
	}
	trustScript, ok := trustPreview.Params["script"].(string)
	if !ok {
		t.Fatalf("expected trust script string, got %T", trustPreview.Params["script"])
	}
	if trustPreview.When != "true" {
		t.Fatalf("expected trust task to be gated by safe_directory, got %q", trustPreview.When)
	}
	for _, got := range []string{trustCheck, trustScript} {
		if !strings.Contains(got, "Convert-ToGitSafeDirectory") {
			t.Fatalf("expected git trust task to normalize safe.directory paths, got:\n%s", got)
		}
		if strings.Contains(got, "safe.directory', '*'") {
			t.Fatalf("expected git trust task not to persist wildcard safe.directory, got:\n%s", got)
		}
	}
	if !strings.Contains(trustScript, "config', '--global', '--add', 'safe.directory', $safePath") {
		t.Fatalf("expected git trust task to persist safe.directory before sync checks, got:\n%s", trustScript)
	}

	preview, err := PreviewTask(plan.Tasks[1], nil)
	if err != nil {
		t.Fatalf("PreviewTask(git sync) returned error: %v", err)
	}
	checkScript, ok := preview.Params["check_script"].(string)
	if !ok {
		t.Fatalf("expected check_script string, got %T", preview.Params["check_script"])
	}
	script, ok := preview.Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", preview.Params["script"])
	}

	if strings.Contains(checkScript, "safe.directory") {
		t.Fatalf("expected sync check script to rely on the preceding trust task, got:\n%s", checkScript)
	}
	if !strings.Contains(script, "$safeDirectory = [System.Convert]::ToBoolean('true')") {
		t.Fatalf("expected git-sync apply script to keep safe.directory enabled by default, got:\n%s", script)
	}
	if !strings.Contains(script, "Convert-ToGitSafeDirectory") {
		t.Fatalf("expected git-sync apply script to normalize safe.directory paths, got:\n%s", script)
	}
	if !strings.Contains(script, "config', '--global', '--add', 'safe.directory', $safePath") {
		t.Fatalf("expected git-sync script to persist safe.directory config, got:\n%s", script)
	}
}

func TestApplyStdlibGitSyncResolvesCredentialEnvSecrets(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"github-token": {File: "secrets/github-token.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("github-token", []byte("ghp_example")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusChanged}}}
	r := New(mt, action.Chain{action.NewEmbeddedResolver(stdlib.FS)}, Config{
		ProjectDir:    dir,
		SecretsConfig: cfg,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	pb := &action.Playbook{
		Name: "git-sync-apply",
		Tasks: []action.Task{{
			Name: "sync private repo",
			Uses: "preflight/git-sync",
			With: map[string]any{
				"repo":          "https://github.com/example/private-repo.git",
				"dest":          "C:\\Exhibits\\App",
				"http_password": "secret:github-token",
			},
		}},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(mt.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(mt.calls))
	}
	env, ok := mt.calls[1].Params["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %T", mt.calls[1].Params["env"])
	}
	if env["PREFLIGHT_GIT_HTTP_PASSWORD"] != "ghp_example" {
		t.Fatalf("expected resolved token in env, got %#v", env["PREFLIGHT_GIT_HTTP_PASSWORD"])
	}
	script, ok := mt.calls[1].Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", mt.calls[1].Params["script"])
	}
	if strings.Contains(script, "ghp_example") {
		t.Fatalf("expected script to avoid embedding resolved token, got:\n%s", script)
	}
}

func TestApplyStdlibWindowsMachineRendersNestedExecutionTimeInputs(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	mt := &mockTarget{
		results: []target.Result{
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
			{Status: target.StatusChanged},
		},
	}
	r := New(mt, resolver, Config{
		TargetVars: map[string]any{
			"hostname": "Gallery-Kiosk-01",
			"timezone": "Eastern Standard Time",
		},
	})
	pb := &action.Playbook{
		Name: "windows-machine",
		Tasks: []action.Task{
			{
				Name: "machine baseline",
				Uses: "preflight/windows-machine",
				With: map[string]any{
					"computer_name": "{{ target.hostname }}",
					"timezone":      "{{ target.timezone }}",
				},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(mt.calls) != 5 {
		t.Fatalf("expected 5 executed tasks, got %d", len(mt.calls))
	}

	checkScript, ok := mt.calls[0].Params["check_script"].(string)
	if !ok {
		t.Fatalf("expected check_script string, got %T", mt.calls[0].Params["check_script"])
	}
	if strings.Contains(checkScript, "{{") {
		t.Fatalf("expected rendered computer name script, got:\n%s", checkScript)
	}
	if !strings.Contains(checkScript, "Gallery-Kiosk-01") {
		t.Fatalf("expected rendered computer name in check script, got:\n%s", checkScript)
	}

	tzScript, ok := mt.calls[1].Params["script"].(string)
	if !ok {
		t.Fatalf("expected script string, got %T", mt.calls[1].Params["script"])
	}
	if strings.Contains(tzScript, "{{") {
		t.Fatalf("expected rendered timezone script, got:\n%s", tzScript)
	}
	if !strings.Contains(tzScript, "Eastern Standard Time") {
		t.Fatalf("expected rendered timezone in script, got:\n%s", tzScript)
	}
}

func TestApplyStdlibWindowsMachineSkipsOptionalTasksWhenInputsOmitted(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	mt := &mockTarget{
		results: []target.Result{
			{Status: target.StatusOK},
			{Status: target.StatusOK},
			{Status: target.StatusOK},
		},
	}
	r := New(mt, resolver, Config{})
	pb := &action.Playbook{
		Name: "windows-machine",
		Tasks: []action.Task{
			{
				Name: "machine baseline",
				Uses: "preflight/windows-machine",
				With: map[string]any{},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	// computer_name and timezone tasks should be skipped; long_paths,
	// ps1_execution_policy, and the default new-network prompt task should execute.
	if len(mt.calls) != 3 {
		t.Fatalf("expected 3 executed tasks (skipped computer_name and timezone), got %d", len(mt.calls))
	}
}

func TestApplyEmitsNewTaskEvents(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusOK}},
	}
	rec := &recordingRenderer{}
	cfg := Config{Renderer: rec}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("emit-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var taskOKs, taskStarted int
	for _, e := range rec.events {
		switch e.(type) {
		case output.TaskOKEvent:
			taskOKs++
		case output.TaskStartedEvent:
			taskStarted++
		}
	}
	if taskOKs != 1 {
		t.Errorf("expected 1 task_ok event, got %d", taskOKs)
	}
	if taskStarted != 1 {
		t.Errorf("expected 1 task_started event, got %d", taskStarted)
	}
}

func TestApplyEmitsTaskOutputEventsWithTargetContext(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusChanged}},
		output:  []string{"line1", "line2"},
	}
	rec := &recordingRenderer{}
	cfg := Config{
		Renderer:   rec,
		TargetName: "gallery-01",
	}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("emit-output-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var taskOutputs []output.TaskOutputEvent
	for _, e := range rec.events {
		if toe, ok := e.(output.TaskOutputEvent); ok {
			taskOutputs = append(taskOutputs, toe)
		}
	}
	if len(taskOutputs) != 2 {
		t.Fatalf("expected 2 task_output events, got %d", len(taskOutputs))
	}
	for i, e := range taskOutputs {
		if e.Target != "gallery-01" {
			t.Fatalf("task_output[%d] target = %q, want %q", i, e.Target, "gallery-01")
		}
		if e.TaskID == "" {
			t.Fatalf("task_output[%d] missing task id", i)
		}
		if e.TaskName != "run echo" {
			t.Fatalf("task_output[%d] task name = %q, want %q", i, e.TaskName, "run echo")
		}
		if len(e.Lines) != 1 || e.Lines[0] != mt.output[i] {
			t.Fatalf("task_output[%d] lines = %v, want [%q]", i, e.Lines, mt.output[i])
		}
	}
}

func TestApplyEmitsRemoteActivityEvents(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusOK}},
	}
	rec := &recordingRenderer{}
	cfg := Config{
		Renderer:   rec,
		TargetName: "gallery-01",
	}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("emit-activity-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var activityStarts, activityResults int
	for _, e := range rec.events {
		switch evt := e.(type) {
		case output.ActivityStartEvent:
			activityStarts++
			if evt.Target != "gallery-01" || evt.Message != "connecting" {
				t.Fatalf("unexpected activity start: %+v", evt)
			}
		case output.ActivityResultEvent:
			activityResults++
			if evt.Target != "gallery-01" || evt.Message != "connecting" || evt.Status != "ok" {
				t.Fatalf("unexpected activity result: %+v", evt)
			}
		}
	}
	if activityStarts != 1 {
		t.Fatalf("expected 1 activity_start event, got %d", activityStarts)
	}
	if activityResults != 1 {
		t.Fatalf("expected 1 activity_result event, got %d", activityResults)
	}
}

func TestApplyDryRun(t *testing.T) {
	mt := &mockTarget{
		results: []target.Result{{Status: target.StatusChanged}},
	}
	cfg := Config{DryRun: true}
	r := New(mt, emptyResolver(), cfg)

	pb := newShellPlaybook("dry-run-test")
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if len(mt.calls) == 0 {
		t.Fatal("expected Execute to be called, got 0 calls")
	}
	if !mt.calls[0].DryRun {
		t.Error("expected dryRun=true in Execute call")
	}
}

func TestApplyResolvesSecretsBeforeExecute(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}

	provider := secrets.NewRepoProvider(dir, config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"autologin-password": {File: "secrets/autologin-password.age"},
		},
	})
	if err := provider.Encrypt("autologin-password", []byte("top-secret")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, emptyResolver(), Config{
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "secret-test",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set secret",
			Module: "shell",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "secret:autologin-password",
				},
				"password": "secret:autologin-password",
			},
		}},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(mt.calls) != 1 {
		t.Fatalf("expected one Execute call, got %d", len(mt.calls))
	}
	if mt.calls[0].Params["password"] != "top-secret" {
		t.Fatalf("expected password param to be resolved, got %#v", mt.calls[0].Params["password"])
	}
	env, ok := mt.calls[0].Params["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %T", mt.calls[0].Params["env"])
	}
	if env["PASSWORD"] != "top-secret" {
		t.Fatalf("expected nested secret ref to resolve, got %#v", env["PASSWORD"])
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{
		LastApplied: time.Now().Truncate(time.Second),
		Tasks:       make(map[string]TaskSnapshot),
	}
	s.RecordTask(TaskSnapshot{
		TaskKey:   "task-0",
		TaskName:  "run echo",
		Status:    target.StatusOK,
		Timestamp: time.Now().Truncate(time.Second),
		ParamHash: "abc123",
		TaskHash:  hashValue(map[string]any{"task_key": "task-0", "task_name": "run echo", "param_hash": "abc123"}),
	})

	if err := s.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}

	if len(loaded.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded.Tasks))
	}
	got := loaded.Tasks["task-0"]
	if got.TaskKey != "task-0" {
		t.Errorf("task key mismatch: got %q", got.TaskKey)
	}
	if got.Status != target.StatusOK {
		t.Errorf("status mismatch: got %q", got.Status)
	}
	if got.ParamHash != "abc123" {
		t.Errorf("param hash mismatch: got %q", got.ParamHash)
	}
}

type staticResolver struct {
	action *action.Action
}

func (r *staticResolver) Resolve(_ context.Context, _ string) (*action.Action, error) {
	return r.action, nil
}
func (r *staticResolver) Name() string { return "static" }

func TestLoadStateMissing(t *testing.T) {
	s, err := LoadState("/nonexistent/path/state.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil State")
	}
	if len(s.Tasks) != 0 {
		t.Errorf("expected empty Tasks, got %d", len(s.Tasks))
	}
}

func TestComparePlannedTasksNoStateMarksAllNew(t *testing.T) {
	comparisons := ComparePlannedTasks([]PlannedTaskState{
		{TaskKey: "task-a", TaskName: "one", ParamHash: "a", TaskHash: "hash-a"},
		{TaskKey: "task-b", TaskName: "two", ParamHash: "b", TaskHash: "hash-b"},
	}, &State{Tasks: map[string]TaskSnapshot{}})

	if len(comparisons) != 2 {
		t.Fatalf("expected 2 comparisons, got %d", len(comparisons))
	}
	for i, comparison := range comparisons {
		if comparison.Status != ComparisonStatusNew {
			t.Fatalf("comparison %d: expected NEW, got %s", i, comparison.Status)
		}
	}
}

func TestComparePlannedTasksMarksKnownChangedAndRemoved(t *testing.T) {
	state := &State{
		Tasks: map[string]TaskSnapshot{
			"task-0": {TaskKey: "task-0", TaskName: "known task", ParamHash: "same", TaskHash: "same", Status: target.StatusOK},
			"task-1": {TaskKey: "task-1", TaskName: "changed task", ParamHash: "old", TaskHash: "old", Status: target.StatusChanged},
			"task-2": {TaskKey: "task-2", TaskName: "removed task", ParamHash: "gone", TaskHash: "gone", Status: target.StatusFailed},
		},
	}

	comparisons := ComparePlannedTasks([]PlannedTaskState{
		{TaskKey: "task-0", TaskName: "known task", ParamHash: "same", TaskHash: "same"},
		{TaskKey: "task-1", TaskName: "changed task", ParamHash: "new", TaskHash: "new"},
	}, state)

	if len(comparisons) != 3 {
		t.Fatalf("expected 3 comparisons, got %d", len(comparisons))
	}
	if comparisons[0].Status != ComparisonStatusUnchanged {
		t.Fatalf("expected first comparison to be UNCHANGED, got %s", comparisons[0].Status)
	}
	if comparisons[1].Status != ComparisonStatusChanged {
		t.Fatalf("expected second comparison to be CHANGED, got %s", comparisons[1].Status)
	}
	if comparisons[2].Status != ComparisonStatusRemoved {
		t.Fatalf("expected third comparison to be REMOVED, got %s", comparisons[2].Status)
	}
	if comparisons[2].RecordedStatus != target.StatusFailed {
		t.Fatalf("expected removed task to keep recorded status, got %s", comparisons[2].RecordedStatus)
	}
}

func TestBuildPlannedTaskStateRendersExecutionTimeTemplates(t *testing.T) {
	plan := &ExecutionPlan{
		PlaybookName: "rendered-state",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "echo {{ target.name }} on {{ facts.os.name }}",
			Module: "shell",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"cmd":  "echo",
				"args": []any{"{{ env.SITE }}", "{{ target.address }}", "{{ facts.os.build }}"},
			},
		}},
	}

	planned, err := BuildPlannedTaskState(context.Background(), plan, &template.RuntimeContext{
		Target: map[string]any{
			"name":    "kiosk-a",
			"address": "10.0.0.1",
		},
		Facts: map[string]any{
			"os": map[string]any{
				"name":  "Windows 11",
				"build": 22631,
			},
		},
		Env: map[string]string{
			"SITE": "lobby",
		},
	}, nil)
	if err != nil {
		t.Fatalf("BuildPlannedTaskState returned error: %v", err)
	}
	if len(planned) != 1 {
		t.Fatalf("expected 1 planned task, got %d", len(planned))
	}
	if planned[0].TaskName != "echo kiosk-a on Windows 11" {
		t.Fatalf("expected rendered task name, got %q", planned[0].TaskName)
	}
	wantHash := ParamHash(map[string]any{
		"cmd":  "echo",
		"args": []any{"lobby", "10.0.0.1", "22631"},
	})
	if planned[0].ParamHash != wantHash {
		t.Fatalf("expected rendered param hash %q, got %q", wantHash, planned[0].ParamHash)
	}
}

func TestBuildPlannedTaskStateResolvesDependenciesByRawTaskName(t *testing.T) {
	plan := &ExecutionPlan{
		PlaybookName: "dep-state",
		Tasks: []*PlanTask{
			{
				ID:     "task-0",
				Name:   "prepare {{ target.name }}",
				Ref:    "prepare",
				Module: "shell",
				Params: map[string]any{"cmd": "echo"},
				Scope:  template.NewScope(),
			},
			{
				ID:        "task-1",
				Name:      "apply",
				Module:    "shell",
				DependsOn: []string{"prepare"},
				Params:    map[string]any{"cmd": "echo"},
				Scope:     template.NewScope(),
			},
		},
	}

	planned, err := BuildPlannedTaskState(context.Background(), plan, &template.RuntimeContext{
		Target: map[string]any{"name": "kiosk-a"},
	}, nil)
	if err != nil {
		t.Fatalf("BuildPlannedTaskState returned error: %v", err)
	}
	if len(planned) != 2 {
		t.Fatalf("expected 2 planned tasks, got %d", len(planned))
	}
	if len(planned[1].DependsOn) != 1 || planned[1].DependsOn[0] != "task-0" {
		t.Fatalf("expected dependency on task-0, got %#v", planned[1].DependsOn)
	}
}

func TestApplySavesStateWithStableDependsOnKeys(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "provision.json")
	mt := &mockTarget{results: []target.Result{{Status: target.StatusChanged}, {Status: target.StatusChanged}}}
	r := New(mt, emptyResolver(), Config{StatePath: statePath})
	plan := &ExecutionPlan{
		PlaybookName: "state-save",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "prepare",
			Ref:    "prepare",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(),
		}, {
			ID:        "task-1",
			Name:      "apply",
			Module:    "shell",
			DependsOn: []string{"prepare"},
			Params:    map[string]any{"cmd": "echo"},
			Scope:     template.NewScope(),
		}},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	recorded := state.Tasks["task-1"]
	if len(recorded.DependsOn) != 1 || recorded.DependsOn[0] != "task-0" {
		t.Fatalf("expected saved depends_on to use stable task keys, got %#v", recorded.DependsOn)
	}
}

func TestApplySavesStateWithParamHashes(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "provision.json")
	mt := &mockTarget{results: []target.Result{{Status: target.StatusChanged}}}
	r := New(mt, emptyResolver(), Config{StatePath: statePath})
	plan := &ExecutionPlan{
		PlaybookName: "state-save",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "shell task",
			Module: "shell",
			Params: map[string]any{"cmd": "echo"},
			Scope:  template.NewScope(),
		}},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	recorded := state.Tasks["task-0"]
	if recorded.ParamHash == "" {
		t.Fatal("expected saved state to include param hash")
	}
	if recorded.ParamHash != ParamHash(plan.Tasks[0].Params) {
		t.Fatalf("expected param hash %q, got %q", ParamHash(plan.Tasks[0].Params), recorded.ParamHash)
	}
}

func TestRunFetchAndStagePhases(t *testing.T) {
	playbook := newShellPlaybook("phase-test")
	if err := New(&mockTarget{}, emptyResolver(), Config{Phase: "fetch"}).Run(context.Background(), playbook); err != nil {
		t.Fatalf("fetch phase: expected nil, got %v", err)
	}

	dir := t.TempDir()
	err := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
	}).Run(context.Background(), playbook)
	if err != nil {
		t.Fatalf("stage phase: expected nil, got %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		t.Fatalf("Glob bundle output: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one staged bundle, got %d", len(matches))
	}
}

func TestRunStageUsesDeclaredPlatformWithoutTargetInfo(t *testing.T) {
	dir := t.TempDir()
	tgt := &targettest.Fake{
		InfoValue: target.TargetInfo{Transport: target.TransportLocal},
		InfoErr:   errors.New("target is offline"),
	}
	r := New(tgt, emptyResolver(), Config{
		Phase:           "stage",
		StagePlatform:   &target.Platform{OS: target.OSFamilyWindows, Arch: "amd64"},
		TargetName:      "TS1",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"registry": noopModule{}},
	})
	playbook := singleTaskPlaybook("registry")

	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("stage with declared platform: %v", err)
	}

	extracted, err := bundle.Extract(mustOneBundlePath(t, dir))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() { _ = extracted.Cleanup() }()
	if got := extracted.Manifest.TargetOS; got != "windows" {
		t.Fatalf("manifest target OS = %q, want windows", got)
	}
	if got := extracted.Manifest.TargetArch; got != "amd64" {
		t.Fatalf("manifest target arch = %q, want amd64", got)
	}
}

func TestRunStageProbesTargetWithoutDeclaredPlatform(t *testing.T) {
	dir := t.TempDir()
	tgt := &targettest.Fake{InfoErr: errors.New("target is offline")}
	r := New(tgt, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
	})

	err := r.Run(context.Background(), newShellPlaybook("probe-target"))
	if err == nil || !strings.Contains(err.Error(), "stage: target info: target is offline") {
		t.Fatalf("stage error = %v, want target info failure", err)
	}
}

func TestStageRejectsPluginForForeignPlatform(t *testing.T) {
	foreignOS := target.OSFamilyWindows
	if runtime.GOOS == "windows" {
		foreignOS = target.OSFamilyLinux
	}
	tests := map[string]struct {
		target   target.Target
		platform *target.Platform
	}{
		"declared": {
			target:   &targettest.Fake{InfoErr: errors.New("must not probe")},
			platform: &target.Platform{OS: foreignOS, Arch: runtime.GOARCH},
		},
		"probed": {
			target: &targettest.Fake{InfoValue: target.TargetInfo{
				OSFamily: foreignOS,
				Arch:     runtime.GOARCH,
			}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r := New(tc.target, emptyResolver(), Config{
				StagePlatform:   tc.platform,
				BundleOutputDir: t.TempDir(),
				ModuleRegistry: target.ModuleRegistry{
					"custom": fakePluggable{path: "/missing/plugin"},
				},
				BundlePlugins: []plugins.LoadedPlugin{{
					Name: "custom",
					Path: "/missing/plugin",
				}},
			})
			plan := &ExecutionPlan{
				PlaybookName: "plugin-bundle",
				Tasks: []*PlanTask{{
					ID:     "task-0",
					Name:   "custom task",
					Module: "custom",
					Scope:  template.NewScope(),
				}},
			}

			err := r.Stage(context.Background(), plan)
			if err == nil || !strings.Contains(err.Error(), "cross-platform plugin staging is not supported") {
				t.Fatalf("stage error = %v, want cross-platform plugin rejection", err)
			}
		})
	}
}

func TestStageBundlesReferencedEncryptedSecrets(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"db-password": {File: "secrets/db-password.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("db-password", []byte("hunter2")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
		ProjectDir:      dir,
		SecretsConfig:   cfg,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "bundle-secret",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set env secret",
			Module: "shell",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "secret:db-password",
				},
			},
		}},
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	bundlePath := mustOneBundlePath(t, dir)
	extracted, err := bundle.Extract(bundlePath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}()

	if extracted.Manifest.SecretMode != bundle.SecretModeEncrypted {
		t.Fatalf("expected encrypted secret mode, got %q", extracted.Manifest.SecretMode)
	}
	if len(extracted.Manifest.SecretEntries) != 1 || extracted.Manifest.SecretEntries[0].Name != "db-password" {
		t.Fatalf("unexpected secret entries: %#v", extracted.Manifest.SecretEntries)
	}
	planBytes, err := os.ReadFile(extracted.PlanPath)
	if err != nil {
		t.Fatalf("ReadFile(plan): %v", err)
	}
	if !strings.Contains(string(planBytes), "secret:db-password") {
		t.Fatalf("expected staged plan to preserve secret ref, got %q", string(planBytes))
	}
	if strings.Contains(string(planBytes), "hunter2") {
		t.Fatalf("expected staged plan to avoid plaintext secret, got %q", string(planBytes))
	}
	encryptedBytes, err := os.ReadFile(filepath.Join(dir, "secrets", "db-password.age"))
	if err != nil {
		t.Fatalf("ReadFile(secret source): %v", err)
	}
	bundledBytes, err := os.ReadFile(filepath.Join(extracted.RootDir, filepath.FromSlash(extracted.Manifest.SecretEntries[0].Path)))
	if err != nil {
		t.Fatalf("ReadFile(bundled secret): %v", err)
	}
	if string(bundledBytes) != string(encryptedBytes) {
		t.Fatalf("expected bundled ciphertext to match source ciphertext")
	}
}

func TestStageBundlesSecretsReferencedByFileContentTemplate(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"app-password": {File: "secrets/app-password.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("app-password", []byte("top-secret")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"file": noopModule{}},
		ProjectDir:      dir,
		SecretsConfig:   cfg,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "bundle-file-template-secret",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "write config",
			Module: "file",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"dest":             "C:\\Exhibits\\app.ini",
				"content_template": "password={{ secret(\"app-password\") }}\n",
			},
		}},
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	extracted, err := bundle.Extract(mustOneBundlePath(t, dir))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}()

	if extracted.Manifest.SecretMode != bundle.SecretModeEncrypted {
		t.Fatalf("expected encrypted secret mode, got %q", extracted.Manifest.SecretMode)
	}
	if len(extracted.Manifest.SecretEntries) != 1 || extracted.Manifest.SecretEntries[0].Name != "app-password" {
		t.Fatalf("unexpected secret entries: %#v", extracted.Manifest.SecretEntries)
	}
	planBytes, err := os.ReadFile(extracted.PlanPath)
	if err != nil {
		t.Fatalf("ReadFile(plan): %v", err)
	}
	if strings.Contains(string(planBytes), "top-secret") {
		t.Fatalf("expected staged plan to avoid plaintext secret, got %q", string(planBytes))
	}
}

func TestStageStdlibGitSyncBundlesCredentialSecrets(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"github-token": {File: "secrets/github-token.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("github-token", []byte("ghp_example")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	r := New(&mockTarget{}, action.Chain{action.NewEmbeddedResolver(stdlib.FS)}, Config{
		Phase:           "stage",
		BundleOutputDir: dir,
		ModuleRegistry:  map[string]target.Module{"powershell": noopModule{}},
		ProjectDir:      dir,
		SecretsConfig:   cfg,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	pb := &action.Playbook{
		Name: "git-sync-secret",
		Tasks: []action.Task{{
			Name: "sync private repo",
			Uses: "preflight/git-sync",
			With: map[string]any{
				"repo":          "https://github.com/example/private-repo.git",
				"dest":          "C:\\Exhibits\\App",
				"ref":           "main",
				"http_password": "secret:github-token",
			},
		}},
	}
	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	bundlePath := mustOneBundlePath(t, dir)
	extracted, err := bundle.Extract(bundlePath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}()

	if extracted.Manifest.SecretMode != bundle.SecretModeEncrypted {
		t.Fatalf("expected encrypted secret mode, got %q", extracted.Manifest.SecretMode)
	}
	if len(extracted.Manifest.SecretEntries) != 1 || extracted.Manifest.SecretEntries[0].Name != "github-token" {
		t.Fatalf("unexpected secret entries: %#v", extracted.Manifest.SecretEntries)
	}
	planBytes, err := os.ReadFile(extracted.PlanPath)
	if err != nil {
		t.Fatalf("ReadFile(plan): %v", err)
	}
	if !strings.Contains(string(planBytes), "secret:github-token") {
		t.Fatalf("expected staged plan to preserve secret ref, got %q", string(planBytes))
	}
	if strings.Contains(string(planBytes), "ghp_example") {
		t.Fatalf("expected staged plan to avoid plaintext token, got %q", string(planBytes))
	}
}

func TestStageRejectsLiteralSecretWithoutPlaintextFlag(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: t.TempDir(),
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
	})
	plan := &ExecutionPlan{
		PlaybookName: "literal-secret",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set literal secret",
			Module: "shell",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "hunter2",
				},
			},
		}},
	}

	err := r.Stage(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "cannot be embedded in a staged bundle") {
		t.Fatalf("expected literal secret stage failure, got %v", err)
	}
}

func TestStageAllowsEmptySecretishValues(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:           "stage",
		BundleOutputDir: t.TempDir(),
		ModuleRegistry:  map[string]target.Module{"shell": noopModule{}},
	})
	plan := &ExecutionPlan{
		PlaybookName: "empty-secretish",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set empty secretish env",
			Module: "shell",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PREFLIGHT_GIT_HTTP_PASSWORD":   "",
					"PREFLIGHT_GIT_SSH_PRIVATE_KEY": "",
				},
			},
		}},
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage returned error for empty secretish values: %v", err)
	}
}

func TestStageAllowsPlaintextSecretsWhenFlagEnabled(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}
	cfg := config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"db-password": {File: "secrets/db-password.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("db-password", []byte("hunter2")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	r := New(&mockTarget{}, emptyResolver(), Config{
		Phase:                         "stage",
		BundleOutputDir:               dir,
		ModuleRegistry:                map[string]target.Module{"shell": noopModule{}},
		ProjectDir:                    dir,
		SecretsConfig:                 cfg,
		AllowPlaintextSecretsInBundle: true,
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "plaintext-secret",
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "set secret env",
			Module: "shell",
			Scope:  template.NewScope(),
			Params: map[string]any{
				"cmd": "echo",
				"env": map[string]any{
					"PASSWORD": "secret:db-password",
					"TOKEN":    "abc123",
				},
			},
		}},
	}

	if err := r.Stage(context.Background(), plan); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	bundlePath := mustOneBundlePath(t, dir)
	extracted, err := bundle.Extract(bundlePath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	defer func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}()

	if extracted.Manifest.SecretMode != bundle.SecretModePlaintext {
		t.Fatalf("expected plaintext secret mode, got %q", extracted.Manifest.SecretMode)
	}
	if len(extracted.Manifest.SecretEntries) != 1 {
		t.Fatalf("expected one bundled plaintext secret, got %#v", extracted.Manifest.SecretEntries)
	}
	info, err := os.Stat(extracted.PlanPath)
	if err != nil {
		t.Fatalf("Stat(plan): %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected plaintext plan mode 0600, got %#o", info.Mode().Perm())
	}
	secretBytes, err := os.ReadFile(filepath.Join(extracted.RootDir, filepath.FromSlash(extracted.Manifest.SecretEntries[0].Path)))
	if err != nil {
		t.Fatalf("ReadFile(secret): %v", err)
	}
	if string(secretBytes) != "hunter2" {
		t.Fatalf("expected plaintext bundled secret, got %q", string(secretBytes))
	}
}

func mustOneBundlePath(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one staged bundle, got %d", len(matches))
	}
	return matches[0]
}

type noopModule struct{}

func (noopModule) Check(_ context.Context, _ map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	return target.CheckResult{}, nil
}
func (noopModule) Apply(_ context.Context, _ map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	return target.ApplyResult{}, nil
}

type fetchableResolver struct {
	actions map[string]*action.Action
	fetched map[string]bool
	calls   []string
}

func (r *fetchableResolver) Name() string { return "fetchable" }

func (r *fetchableResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	if !r.fetched[ref] {
		return nil, &action.RemoteCacheMissError{Ref: ref}
	}
	return a, nil
}

func (r *fetchableResolver) Fetch(_ context.Context, ref string) (*action.FetchResult, error) {
	r.calls = append(r.calls, ref)
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	if r.fetched == nil {
		r.fetched = make(map[string]bool)
	}
	r.fetched[ref] = true
	return &action.FetchResult{
		Entry:  action.LockEntry{Ref: ref, SHA: "sha-" + ref, Pinned: ref},
		Action: a,
	}, nil
}

func TestRunFetchesRemoteDependenciesBeforePlanningAndApply(t *testing.T) {
	rootRef := "github.com/acme/actions/root@v1"
	childRef := "github.com/acme/actions/child@v1"
	resolver := &fetchableResolver{
		actions: map[string]*action.Action{
			rootRef: {
				Name: "root",
				Tasks: []action.Task{{
					Name: "child",
					Uses: childRef,
				}},
			},
			childRef: {
				Name: "child",
				Tasks: []action.Task{{
					Name:          "echo",
					InlineModules: map[string]map[string]any{"shell": {"cmd": "echo hello"}},
				}},
			},
		},
		fetched: make(map[string]bool),
	}

	playbook := &action.Playbook{
		Name: "remote",
		Tasks: []action.Task{{
			Name: "root",
			Uses: rootRef,
		}},
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, action.Chain{resolver}, Config{})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("expected 2 fetch calls, got %d", len(resolver.calls))
	}
	if resolver.calls[0] != rootRef || resolver.calls[1] != childRef {
		t.Fatalf("unexpected fetch order: %#v", resolver.calls)
	}
	if len(mt.calls) != 1 || mt.calls[0].Module != "shell" {
		t.Fatalf("expected one shell execution, got %#v", mt.calls)
	}
}

func TestRunFetchPhaseStopsBeforeApply(t *testing.T) {
	rootRef := "github.com/acme/actions/root@v1"
	resolver := &fetchableResolver{
		actions: map[string]*action.Action{
			rootRef: {
				Name: "root",
				Tasks: []action.Task{{
					Name:          "echo",
					InlineModules: map[string]map[string]any{"shell": {"cmd": "echo hello"}},
				}},
			},
		},
		fetched: make(map[string]bool),
	}

	playbook := &action.Playbook{
		Name:  "remote",
		Tasks: []action.Task{{Name: "root", Uses: rootRef}},
	}
	mt := &mockTarget{}
	r := New(mt, action.Chain{resolver}, Config{Phase: "fetch"})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run(fetch): %v", err)
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("expected 1 fetch call, got %d", len(resolver.calls))
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no target execution during fetch phase, got %#v", mt.calls)
	}
}

// localActionResolver handles a fixed set of non-remote refs without any fetch step,
// simulating a local or embedded action resolver.
type localActionResolver struct {
	actions map[string]*action.Action
}

func (r *localActionResolver) Name() string { return "local" }
func (r *localActionResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func TestRunFetchesRemoteDepsNestedUnderLocalAction(t *testing.T) {
	localRef := "local/wrapper"
	remoteRef := "github.com/acme/actions/child@v1"

	localAction := &action.Action{
		Name:  "wrapper",
		Tasks: []action.Task{{Name: "child", Uses: remoteRef}},
	}
	remoteAction := &action.Action{
		Name: "child",
		Tasks: []action.Task{{
			Name:          "echo",
			InlineModules: map[string]map[string]any{"shell": {"cmd": "echo hello"}},
		}},
	}

	fr := &fetchableResolver{
		actions: map[string]*action.Action{remoteRef: remoteAction},
		fetched: make(map[string]bool),
	}
	chain := action.Chain{
		&localActionResolver{actions: map[string]*action.Action{localRef: localAction}},
		fr,
	}

	playbook := &action.Playbook{
		Name: "local-to-remote",
		Tasks: []action.Task{{
			Name: "wrapper",
			Uses: localRef,
		}},
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, chain, Config{})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fr.calls) != 1 || fr.calls[0] != remoteRef {
		t.Errorf("expected fetch of %q, got %v", remoteRef, fr.calls)
	}
	if len(mt.calls) != 1 || mt.calls[0].Module != "shell" {
		t.Errorf("expected one shell execution, got %#v", mt.calls)
	}
}

func TestRunFetchesRemoteDepsNestedUnderStdlibAction(t *testing.T) {
	stdlibRef := "preflight/baseline"
	remoteRef := "github.com/acme/actions/plugin@v3"

	stdlibAction := &action.Action{
		Name:  "baseline",
		Tasks: []action.Task{{Name: "plugin", Uses: remoteRef}},
	}
	remoteAction := &action.Action{
		Name: "plugin",
		Tasks: []action.Task{{
			Name:          "run",
			InlineModules: map[string]map[string]any{"shell": {"cmd": "plugin.exe"}},
		}},
	}

	fr := &fetchableResolver{
		actions: map[string]*action.Action{remoteRef: remoteAction},
		fetched: make(map[string]bool),
	}
	chain := action.Chain{
		&localActionResolver{actions: map[string]*action.Action{stdlibRef: stdlibAction}},
		fr,
	}

	playbook := &action.Playbook{
		Name: "stdlib-to-remote",
		Tasks: []action.Task{{
			Name: "baseline",
			Uses: stdlibRef,
		}},
	}

	mt := &mockTarget{results: []target.Result{{Status: target.StatusOK}}}
	r := New(mt, chain, Config{})
	if err := r.Run(context.Background(), playbook); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fr.calls) != 1 || fr.calls[0] != remoteRef {
		t.Errorf("expected fetch of %q, got %v", remoteRef, fr.calls)
	}
	if len(mt.calls) != 1 || mt.calls[0].Module != "shell" {
		t.Errorf("expected one shell execution, got %#v", mt.calls)
	}
}

func TestRunPlanPhaseStopsBeforeApply(t *testing.T) {
	mt := &mockTarget{}
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.json")

	r := New(mt, emptyResolver(), Config{
		Phase:     "plan",
		StatePath: statePath,
	})
	pb := newShellPlaybook("plan-only")
	if err := r.Run(context.Background(), pb); err != nil {
		t.Fatalf("Run(plan): %v", err)
	}
	if len(mt.calls) != 0 {
		t.Fatalf("expected no target execution during plan phase, got %#v", mt.calls)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no state file to be written during plan phase, but stat returned: %v", err)
	}
}

func TestTargetNameNeverReturnsLocalhost(t *testing.T) {
	// When no TargetName is configured and TargetVars is empty, targetName()
	// must not return "localhost" — that would silently claim a local identity.
	r := &Runner{config: Config{}}
	got := r.targetName()
	if got == "localhost" {
		t.Errorf("targetName() returned %q with no config; expected empty string or another sentinel, not %q", got, "localhost")
	}

	// Verify it returns empty string specifically.
	if got != "" {
		t.Errorf("targetName() = %q, want %q when unconfigured", got, "")
	}
}

func TestTargetNamePrefersExplicitConfig(t *testing.T) {
	r := &Runner{config: Config{TargetName: "exhibit-01"}}
	if got := r.targetName(); got != "exhibit-01" {
		t.Errorf("targetName() = %q, want %q", got, "exhibit-01")
	}
}

func TestTargetNameFallsBackToTargetVars(t *testing.T) {
	r := &Runner{config: Config{TargetVars: map[string]any{"name": "kiosk-pc"}}}
	if got := r.targetName(); got != "kiosk-pc" {
		t.Errorf("targetName() = %q, want %q", got, "kiosk-pc")
	}

	r2 := &Runner{config: Config{TargetVars: map[string]any{"hostname": "gallery-01"}}}
	if got := r2.targetName(); got != "gallery-01" {
		t.Errorf("targetName() = %q, want %q", got, "gallery-01")
	}
}

func TestNewTaskSnapshotNilDAGProducesEmptyDependsOn(t *testing.T) {
	pt := &PlanTask{
		ID:        "task-1",
		Name:      "dependent task",
		Module:    "shell",
		DependsOn: []string{"failing task"},
		Params:    map[string]any{"cmd": "echo"},
	}

	snap := newTaskSnapshot(pt, pt.Name, pt.Params, pt.Params, nil, nil, target.StatusSkipped, "dependency-failed", nil)
	if len(snap.DependsOn) != 0 {
		t.Fatalf("expected empty DependsOn when dag=nil, got %v", snap.DependsOn)
	}
	if snap.Status != target.StatusSkipped {
		t.Fatalf("expected StatusSkipped, got %v", snap.Status)
	}
	if snap.Message != "dependency-failed" {
		t.Fatalf("expected message %q, got %q", "dependency-failed", snap.Message)
	}
}

func TestNewTaskSnapshotWithDAGResolvesDependencyIDs(t *testing.T) {
	taskA := &PlanTask{ID: "task-0", Name: "failing task", Module: "shell"}
	taskB := &PlanTask{
		ID:        "task-1",
		Name:      "dependent task",
		Module:    "shell",
		DependsOn: []string{"failing task"},
		Params:    map[string]any{"cmd": "echo"},
	}

	dag, err := BuildDAG([]*PlanTask{taskA, taskB})
	if err != nil {
		t.Fatalf("BuildDAG error: %v", err)
	}

	snap := newTaskSnapshot(taskB, taskB.Name, taskB.Params, taskB.Params, nil, nil, target.StatusOK, "", dag)
	if len(snap.DependsOn) != 1 || snap.DependsOn[0] != "task-0" {
		t.Fatalf("expected DependsOn=[task-0], got %v", snap.DependsOn)
	}
}
