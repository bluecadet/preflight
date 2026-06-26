package template

import (
	"maps"
	"testing"
)

func TestScope_EndToEnd_RenderTaskOutput(t *testing.T) {
	// Simulate the plan-time scope construction: four var layers merged
	// (project vars, inventory vars, playbook vars, CLI vars).
	projectVars := map[string]any{
		"site":     "Lobby",
		"timezone": "Eastern Standard Time",
	}
	inventoryVars := map[string]any{
		"device_name": "Gallery-Kiosk-01",
		"site":        "Lobby",
	}
	playbookVars := map[string]any{
		"app_port": 8080,
	}
	cliVars := map[string]any{
		"debug": true,
	}

	root := NewScope(projectVars, inventoryVars, playbookVars, cliVars)

	// Verify layer precedence: project's site was "Lobby", inventory also says
	// "Lobby" (no override), playbook and CLI don't touch it.
	if root.Vars["site"] != "Lobby" {
		t.Fatalf("expected site=Lobby, got %v", root.Vars["site"])
	}
	if root.Vars["device_name"] != "Gallery-Kiosk-01" {
		t.Fatalf("expected device_name=Gallery-Kiosk-01, got %v", root.Vars["device_name"])
	}
	if root.Vars["debug"] != true {
		t.Fatalf("expected debug=true (CLI wins), got %v", root.Vars["debug"])
	}

	// Simulate composition: an action with input defaults + user-provided With values.
	actionInputDefaults := map[string]any{
		"timezone": "UTC", // default, overridden by user
		"retry":    false, // default, kept
	}
	userWith := map[string]any{
		"timezone": "{{ vars.timezone }}", // overrides default
		"app_port": "{{ vars.app_port }}", // passes through
	}

	// Derive a child scope (composition boundary).
	overlay := make(map[string]any)
	maps.Copy(overlay, actionInputDefaults)
	// Render user With values against the parent scope (BindPartial preserves unknowns).
	for k, v := range userWith {
		if s, ok := v.(string); ok {
			rendered, err := root.Engine(nil, BindPartial).Render(s)
			if err != nil {
				t.Fatalf("render user input %q: %v", k, err)
			}
			overlay[k] = rendered
		} else {
			overlay[k] = v
		}
	}

	child := NewDerivedScope(root, overlay)

	// Verify composition: child has parent vars + derived overlay.
	if child.Vars["device_name"] != "Gallery-Kiosk-01" {
		t.Fatalf("expected child device_name from parent, got %v", child.Vars["device_name"])
	}
	if child.Vars["timezone"] != "Eastern Standard Time" {
		t.Fatalf("expected timezone=Eastern Standard Time (overridden default), got %v", child.Vars["timezone"])
	}
	if child.Vars["retry"] != false {
		t.Fatalf("expected retry=false (from default), got %v", child.Vars["retry"])
	}
	if child.Vars["app_port"] != "8080" {
		t.Fatalf("expected app_port=8080 (passed through), got %v", child.Vars["app_port"])
	}

	// Simulate apply-time binding: attach a fake RuntimeContext.
	rt := &RuntimeContext{
		Target: map[string]any{
			"hostname": "gallery-01",
			"ip":       "10.0.0.42",
		},
		Facts: map[string]any{
			"os": map[string]any{
				"name":  "Windows 11",
				"build": "22631",
			},
		},
		Env: map[string]string{
			"COMPUTERNAME": "GALLERY-01",
			"SITE":         "Lobby",
		},
	}

	// Render task params against the child scope with Bind mode (full apply-time).
	eng := child.Engine(rt, Bind)
	params := map[string]any{
		"hostname":    "{{ target.hostname }}",
		"device":      "{{ vars.device_name }}",
		"timezone":    "{{ vars.timezone }}",
		"os_build":    "{{ facts.os.build }}",
		"env_site":    "{{ env.SITE }}",
		"app_port":    "{{ vars.app_port }}",
		"debug":       "{{ vars.debug }}",
		"description": "{{ vars.device_name }} on {{ target.hostname }} ({{ facts.os.name }})",
	}
	rendered, err := eng.RenderMap(params)
	if err != nil {
		t.Fatalf("RenderMap: %v", err)
	}

	if rendered["hostname"] != "gallery-01" {
		t.Errorf("hostname: got %q, want %q", rendered["hostname"], "gallery-01")
	}
	if rendered["device"] != "Gallery-Kiosk-01" {
		t.Errorf("device: got %q, want %q", rendered["device"], "Gallery-Kiosk-01")
	}
	if rendered["timezone"] != "Eastern Standard Time" {
		t.Errorf("timezone: got %q, want %q", rendered["timezone"], "Eastern Standard Time")
	}
	if rendered["os_build"] != "22631" {
		t.Errorf("os_build: got %q, want %q", rendered["os_build"], "22631")
	}
	if rendered["env_site"] != "Lobby" {
		t.Errorf("env_site: got %q, want %q", rendered["env_site"], "Lobby")
	}
	if rendered["app_port"] != "8080" {
		t.Errorf("app_port: got %q, want %q", rendered["app_port"], "8080")
	}
	if rendered["debug"] != "true" {
		t.Errorf("debug: got %q, want %q", rendered["debug"], "true")
	}
	desc := "Gallery-Kiosk-01 on gallery-01 (Windows 11)"
	if rendered["description"] != desc {
		t.Errorf("description: got %q, want %q", rendered["description"], desc)
	}
}

func TestScope_BindPartialPreservesUnknown(t *testing.T) {
	root := NewScope(map[string]any{"known": "value"})
	rt := &RuntimeContext{
		Target: map[string]any{"hostname": "my-pc"},
	}

	// BindPartial should preserve unknown facts.* and env.* references.
	eng := root.Engine(rt, BindPartial)
	got, err := eng.Render("{{ vars.known }} {{ target.hostname }} {{ facts.os.build }} {{ env.PATH }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "value my-pc {{ facts.os.build }} {{ env.PATH }}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScope_BindErrorsOnMissingVars(t *testing.T) {
	root := NewScope(map[string]any{})
	eng := root.Engine(nil, Bind)
	if _, err := eng.Render("{{ vars.missing }}"); err == nil {
		t.Fatal("expected error for missing vars reference in Bind mode")
	}
}

func TestScope_BindPartialStillErrorsOnMissingVars(t *testing.T) {
	root := NewScope(map[string]any{})
	eng := root.Engine(nil, BindPartial)
	if _, err := eng.Render("{{ vars.missing }}"); err == nil {
		t.Fatal("expected error for missing vars reference even in BindPartial mode")
	}
}

func TestScope_NewScopeDeepMerges(t *testing.T) {
	// Layer ordering: later layers win.
	s := NewScope(
		map[string]any{"a": 1, "nested": map[string]any{"x": "from-first"}},
		map[string]any{"b": 2, "nested": map[string]any{"y": "from-second"}},
	)
	if s.Vars["a"] != 1 {
		t.Errorf("a: got %v, want 1", s.Vars["a"])
	}
	if s.Vars["b"] != 2 {
		t.Errorf("b: got %v, want 2", s.Vars["b"])
	}
	nested, ok := s.Vars["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested is not a map: %T", s.Vars["nested"])
	}
	if nested["x"] != "from-first" {
		t.Errorf("nested.x: got %v, want from-first", nested["x"])
	}
	if nested["y"] != "from-second" {
		t.Errorf("nested.y: got %v, want from-second", nested["y"])
	}
}

func TestScope_NewDerivedScopeShallowOverlay(t *testing.T) {
	parent := NewScope(
		map[string]any{"parent_key": "parent_val", "shared": map[string]any{"inner": "from_parent"}},
	)
	overlay := map[string]any{"child_key": "child_val", "shared": "scalar_override"}
	child := NewDerivedScope(parent, overlay)

	if child.Vars["parent_key"] != "parent_val" {
		t.Errorf("parent_key: got %v, want parent_val", child.Vars["parent_key"])
	}
	if child.Vars["shared"] != "scalar_override" {
		t.Errorf("shared: got %v, want scalar_override (scalar overwrites map)", child.Vars["shared"])
	}
	if child.Vars["child_key"] != "child_val" {
		t.Errorf("child_key: got %v, want child_val", child.Vars["child_key"])
	}
}

func TestScope_RuntimeContextIsolation(t *testing.T) {
	// Ensure runtime context is not mutated by rendering.
	root := NewScope(map[string]any{"name": "test"})
	rt := &RuntimeContext{
		Target: map[string]any{"hostname": "pc-01"},
		Facts:  map[string]any{"os": "Windows"},
		Env:    map[string]string{"PATH": "/usr/bin"},
	}

	eng := root.Engine(rt, Bind)
	got, err := eng.Render("{{ vars.name }} on {{ target.hostname }} ({{ facts.os }}, PATH={{ env.PATH }})")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "test on pc-01 (Windows, PATH=/usr/bin)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScope_EngineRendersVars(t *testing.T) {
	s := NewScope(map[string]any{"key": "value", "num": 42})
	data, err := s.Engine(nil, Bind).Render("{{ vars.key }} {{ vars.num }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != "value 42" {
		t.Errorf("got %q, want %q", data, "value 42")
	}
}
