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

func TestRender_SecretFunction(t *testing.T) {
	e := New(nil).WithSecretLookup(func(name string) (string, error) {
		if name != "app-password" {
			t.Fatalf("unexpected secret name %q", name)
		}
		return "top-secret", nil
	})

	got, err := e.Render(`password={{ secret("app-password") }}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "password=top-secret" {
		t.Errorf("got %q, want %q", got, "password=top-secret")
	}
}

func TestRender_PreserveSecretFunction(t *testing.T) {
	e := New(map[string]any{"prefix": "password"}).WithPreserveSecretRefs()

	got, err := e.Render(`{{ vars.prefix }}={{ secret("app-password") }}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `password={{ secret("app-password") }}` {
		t.Errorf("got %q", got)
	}
}

func TestSecretRefNames(t *testing.T) {
	got := SecretRefNames(`a {{ secret("app-password") }} b {{ secret("api_key") }} c {{ secret("app-password") }}`)
	want := []string{"app-password", "api_key"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestSecretRefNamesIgnoresDotNotation(t *testing.T) {
	if got := SecretRefNames(`{{ secret.app-password }}`); len(got) != 0 {
		t.Fatalf("expected dot notation to be ignored, got %#v", got)
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

func TestRenderBool_ComparisonOperators(t *testing.T) {
	cases := []struct {
		name  string
		vars  map[string]any
		expr  string
		want  bool
		isErr bool
	}{
		// String equality / inequality
		{name: "eq_match", vars: map[string]any{"os": "windows"}, expr: "{{ vars.os == 'windows' }}", want: true},
		{name: "eq_no_match", vars: map[string]any{"os": "linux"}, expr: "{{ vars.os == 'windows' }}", want: false},
		{name: "neq_match", vars: map[string]any{"os": "linux"}, expr: "{{ vars.os != 'windows' }}", want: true},
		{name: "neq_no_match", vars: map[string]any{"os": "windows"}, expr: "{{ vars.os != 'windows' }}", want: false},
		{name: "eq_empty_string", vars: map[string]any{"version": ""}, expr: "{{ vars.version != '' }}", want: false},
		{name: "neq_empty_nonempty", vars: map[string]any{"version": "1.0"}, expr: "{{ vars.version != '' }}", want: true},
		// Numeric comparisons
		{name: "gt_true", vars: map[string]any{"count": "10"}, expr: "{{ vars.count > 5 }}", want: true},
		{name: "gt_false", vars: map[string]any{"count": "3"}, expr: "{{ vars.count > 5 }}", want: false},
		{name: "gte_equal", vars: map[string]any{"count": "5"}, expr: "{{ vars.count >= 5 }}", want: true},
		{name: "gte_greater", vars: map[string]any{"count": "6"}, expr: "{{ vars.count >= 5 }}", want: true},
		{name: "lt_true", vars: map[string]any{"count": "3"}, expr: "{{ vars.count < 5 }}", want: true},
		{name: "lt_false", vars: map[string]any{"count": "7"}, expr: "{{ vars.count < 5 }}", want: false},
		{name: "lte_equal", vars: map[string]any{"count": "5"}, expr: "{{ vars.count <= 5 }}", want: true},
		// Var vs var
		{name: "vars_eq_vars", vars: map[string]any{"a": "hello", "b": "hello"}, expr: "{{ vars.a == vars.b }}", want: true},
		{name: "vars_neq_vars", vars: map[string]any{"a": "hello", "b": "world"}, expr: "{{ vars.a == vars.b }}", want: false},
		// Numeric type (int stored as int, not string)
		{name: "int_var_gt", vars: map[string]any{"count": 10}, expr: "{{ vars.count > 5 }}", want: true},
		// Non-numeric operand for numeric op is an error
		{name: "non_numeric_gt", vars: map[string]any{"os": "windows"}, expr: "{{ vars.os > 5 }}", want: false, isErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New(tc.vars)
			got, err := e.RenderBool(tc.expr)
			if tc.isErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("RenderBool(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestRender_TernaryOperator(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]any
		expr string
		want string
	}{
		{
			name: "true branch selected",
			vars: map[string]any{"flag": true},
			expr: "{{ vars.flag ? 1 : 2 }}",
			want: "1",
		},
		{
			name: "false branch selected",
			vars: map[string]any{"flag": false},
			expr: "{{ vars.flag ? 1 : 2 }}",
			want: "2",
		},
		{
			name: "comparison condition true",
			vars: map[string]any{"mode": "light"},
			expr: "{{ vars.mode == 'light' ? 1 : 0 }}",
			want: "1",
		},
		{
			name: "comparison condition false",
			vars: map[string]any{"mode": "dark"},
			expr: "{{ vars.mode == 'light' ? 1 : 0 }}",
			want: "0",
		},
		{
			name: "string branch values",
			vars: map[string]any{"ok": "yes"},
			expr: "{{ vars.ok ? 'enabled' : 'disabled' }}",
			want: "enabled",
		},
		{
			name: "zero is falsy",
			vars: map[string]any{"n": "0"},
			expr: "{{ vars.n ? 'yes' : 'no' }}",
			want: "no",
		},
		{
			name: "var reference in branch",
			vars: map[string]any{"flag": true, "a": "alpha", "b": "beta"},
			expr: "{{ vars.flag ? vars.a : vars.b }}",
			want: "alpha",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New(tc.vars)
			got, err := e.Render(tc.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestRenderBool_ComparisonUndefinedVar(t *testing.T) {
	e := New(map[string]any{})
	// Undefined vars.* should still error even in a comparison.
	_, err := e.RenderBool("{{ vars.missing == 'x' }}")
	if err == nil {
		t.Fatal("expected error for undefined vars reference in comparison")
	}
}

func TestRenderBool_ComparisonUnknownFactsIsFalse(t *testing.T) {
	e := New(nil)

	got, err := e.RenderBool("{{ facts.os.build == '19041' }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Fatal("expected false when comparing against unresolved facts")
	}
}

func TestRender_PreserveUnknownSelectedTernaryBranch(t *testing.T) {
	e := New(map[string]any{
		"use_target": "yes",
	}).WithPreserveUnknown()

	got, err := e.Render("{{ vars.use_target ? target.hostname : 'fallback' }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "{{ vars.use_target ? target.hostname : 'fallback' }}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderMap_WholeValuePreserveUnknown(t *testing.T) {
	e := New(nil).WithPreserveUnknown()

	got, err := e.RenderMap(map[string]any{
		"hostname": "{{ target.hostname }}",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["hostname"] != "{{ target.hostname }}" {
		t.Errorf("hostname = %v, want {{ target.hostname }}", got["hostname"])
	}
}
