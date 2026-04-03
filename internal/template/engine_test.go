package template

import (
	"testing"
)

func TestRender_SimpleVar(t *testing.T) {
	e := New(map[string]any{
		"foo": "bar",
	})
	got, err := e.Render("{{ vars.foo }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bar" {
		t.Errorf("got %q, want %q", got, "bar")
	}
}

func TestRender_NestedVar(t *testing.T) {
	e := New(map[string]any{
		"nested": map[string]any{
			"key": "value",
		},
	})
	got, err := e.Render("{{ vars.nested.key }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
}

func TestRender_EnvVar(t *testing.T) {
	e := New(nil).WithEnv(map[string]string{
		"PATH": "/usr/bin:/bin",
	})
	got, err := e.Render("{{ env.PATH }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/bin:/bin" {
		t.Errorf("got %q, want %q", got, "/usr/bin:/bin")
	}
}

func TestRender_UnknownVar(t *testing.T) {
	e := New(map[string]any{})
	if _, err := e.Render("{{ vars.missing }}"); err == nil {
		t.Fatal("expected error for undefined vars reference")
	}
}

func TestRender_PreserveUnknown(t *testing.T) {
	e := New(map[string]any{
		"known": "value",
	}).WithPreserveUnknown()

	got, err := e.Render("{{ vars.known }} {{ facts.os.build }} {{ target.hostname }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "value {{ facts.os.build }} {{ target.hostname }}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRender_PreserveUnknownStillErrorsForMissingVars(t *testing.T) {
	e := New(map[string]any{}).WithPreserveUnknown()

	if _, err := e.Render("{{ vars.missing }}"); err == nil {
		t.Fatal("expected missing vars reference to fail even with preserve unknown enabled")
	}
}

func TestRender_MultipleExpressions(t *testing.T) {
	e := New(map[string]any{
		"name": "world",
	}).WithEnv(map[string]string{"GREETING": "hello"})
	got, err := e.Render("{{ env.GREETING }}, {{ vars.name }}!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello, world!" {
		t.Errorf("got %q, want %q", got, "hello, world!")
	}
}

func TestRender_RecursiveVarResolution(t *testing.T) {
	e := New(map[string]any{
		"name":        "{{ vars.device_name }}",
		"greeting":    "Hello {{ vars.name }}",
		"device_name": "Gallery-Kiosk-01",
	})

	got, err := e.Render("{{ vars.greeting }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hello Gallery-Kiosk-01" {
		t.Errorf("got %q, want %q", got, "Hello Gallery-Kiosk-01")
	}
}

func TestRender_RecursiveTargetResolution(t *testing.T) {
	e := New(map[string]any{
		"name": "{{ target.hostname }}",
	}).WithTarget(map[string]any{
		"hostname": "gallery-01",
	})

	got, err := e.Render("{{ vars.name }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gallery-01" {
		t.Errorf("got %q, want %q", got, "gallery-01")
	}
}

func TestRender_NoPlaceholders(t *testing.T) {
	e := New(nil)
	s := "no templates here"
	got, err := e.Render(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != s {
		t.Errorf("got %q, want %q", got, s)
	}
}

func TestRender_TargetAndFacts(t *testing.T) {
	e := New(nil).
		WithTarget(map[string]any{"hostname": "pc-01"}).
		WithFacts(map[string]any{
			"os": map[string]any{"build": "19041"},
		})

	got, err := e.Render("host={{ target.hostname }} build={{ facts.os.build }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "host=pc-01 build=19041"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBool(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"yes", true},
		{"no", false},
		{"", false},
		{"Gallery-Kiosk-01", true},
		{"Eastern Standard Time", true},
	}
	e := New(nil)
	for _, tc := range cases {
		got, err := e.RenderBool(tc.input)
		if err != nil {
			t.Errorf("RenderBool(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("RenderBool(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestRenderBool_ViaVar(t *testing.T) {
	e := New(map[string]any{"flag": "true"})
	got, err := e.RenderBool("{{ vars.flag }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true")
	}
}

func TestRenderMap(t *testing.T) {
	e := New(map[string]any{
		"dest": "/opt/app",
		"mode": "0755",
	})
	input := map[string]any{
		"path":    "{{ vars.dest }}",
		"mode":    "{{ vars.mode }}",
		"version": 42, // non-string — passed through unchanged
	}
	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["path"] != "/opt/app" {
		t.Errorf("path: got %v, want /opt/app", got["path"])
	}
	if got["mode"] != "0755" {
		t.Errorf("mode: got %v, want 0755", got["mode"])
	}
	if got["version"] != 42 {
		t.Errorf("version: got %v, want 42", got["version"])
	}
}

func TestRenderMap_WholeValueList(t *testing.T) {
	pkgs := []any{
		map[string]any{"id": "Microsoft.VisualStudioCode", "version": "1.85.0"},
		map[string]any{"id": "Git.Git"},
	}
	e := New(map[string]any{"packages": pkgs})
	input := map[string]any{
		"packages": "{{ vars.packages }}",
	}
	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := got["packages"].([]any)
	if !ok {
		t.Fatalf("packages is not []any: %T", got["packages"])
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(result))
	}
	first, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("packages[0] is not map[string]any: %T", result[0])
	}
	if first["id"] != "Microsoft.VisualStudioCode" {
		t.Errorf("packages[0].id = %v, want Microsoft.VisualStudioCode", first["id"])
	}
}

func TestRenderMap_WholeValueMap(t *testing.T) {
	cfg := map[string]any{"timeout": 30, "retry": true}
	e := New(map[string]any{"config": cfg})
	input := map[string]any{"config": "{{ vars.config }}"}
	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := got["config"].(map[string]any)
	if !ok {
		t.Fatalf("config is not map[string]any: %T", got["config"])
	}
	if result["timeout"] != 30 {
		t.Errorf("config.timeout = %v, want 30", result["timeout"])
	}
}

func TestRenderMap_WholeValueScalarStringifies(t *testing.T) {
	// Integers and booleans in a string template field should still stringify.
	// A user who writes `ac_value: "{{ vars.count }}"` expects a string result.
	e := New(map[string]any{"count": 5, "flag": true})
	input := map[string]any{
		"ac_value": "{{ vars.count }}",
		"enabled":  "{{ vars.flag }}",
	}
	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["ac_value"] != "5" {
		t.Errorf("ac_value = %v (%T), want \"5\"", got["ac_value"], got["ac_value"])
	}
	if got["enabled"] != "true" {
		t.Errorf("enabled = %v (%T), want \"true\"", got["enabled"], got["enabled"])
	}
}

func TestRenderMap_WholeValueString_StillRecurses(t *testing.T) {
	e := New(map[string]any{
		"name":   "{{ vars.device }}",
		"device": "Kiosk-01",
	})
	input := map[string]any{"label": "{{ vars.name }}"}
	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["label"] != "Kiosk-01" {
		t.Errorf("label = %v, want Kiosk-01", got["label"])
	}
}

func TestRenderMap_Nested(t *testing.T) {
	e := New(map[string]any{"val": "x"})
	input := map[string]any{
		"outer": map[string]any{
			"inner": "{{ vars.val }}",
		},
	}
	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outer, ok := got["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer is not a map: %T", got["outer"])
	}
	if outer["inner"] != "x" {
		t.Errorf("inner: got %v, want x", outer["inner"])
	}
}

func TestRenderMap_ListItems(t *testing.T) {
	e := New(map[string]any{
		"exe":     `C:\Program Files\App\run.exe`,
		"ac":      "0",
		"dc":      "15",
		"profile": "machine",
	})
	input := map[string]any{
		"settings": []any{
			map[string]any{
				"subgroup": "SUB_VIDEO",
				"setting":  "VIDEOIDLE",
				"ac_value": "{{ vars.ac }}",
				"dc_value": "{{ vars.dc }}",
			},
		},
		"command": []any{"{{ vars.exe }}", "--profile", "{{ vars.profile }}"},
	}

	got, err := e.RenderMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	settings, ok := got["settings"].([]any)
	if !ok {
		t.Fatalf("settings is not a list: %T", got["settings"])
	}
	if len(settings) != 1 {
		t.Fatalf("expected 1 setting, got %d", len(settings))
	}
	setting, ok := settings[0].(map[string]any)
	if !ok {
		t.Fatalf("settings[0] is not a map: %T", settings[0])
	}
	if setting["ac_value"] != "0" || setting["dc_value"] != "15" {
		t.Fatalf("unexpected rendered settings: %#v", setting)
	}

	command, ok := got["command"].([]any)
	if !ok {
		t.Fatalf("command is not a list: %T", got["command"])
	}
	want := []string{`C:\Program Files\App\run.exe`, "--profile", "machine"}
	if len(command) != len(want) {
		t.Fatalf("expected %d command items, got %d", len(want), len(command))
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command[%d] = %#v, want %#v", i, command[i], want[i])
		}
	}
}
