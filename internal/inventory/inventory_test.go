package inventory

import (
	"testing"

	"github.com/bluecadet/preflight/internal/maputil"
	"github.com/bluecadet/preflight/internal/target"
)

func TestTransportAliasesTargetTransport(t *testing.T) {
	inventoryTransport := TransportSSH
	targetTransport := target.Transport(inventoryTransport)

	if targetTransport != target.TransportSSH {
		t.Fatalf("expected inventory transport to reuse target transport constants, got %q", targetTransport)
	}
}

// baseInventory builds a small inventory used across several tests.
//
// Groups:
//
//	all   — vars: {env: prod, nested: {a: 1}}
//	web   — hosts: [web01, shared01]; vars: {role: web, nested: {b: 2}}
//	cache — hosts: [shared01];        vars: {role: cache, nested: {c: 3}}
//
// shared01 appears in both "web" and "cache".
func baseInventory() *Inventory {
	web01 := Host{Name: "web01", Vars: map[string]any{"host_var": "web01_val"}}
	shared01 := Host{Name: "shared01", Vars: map[string]any{"host_var": "shared_val"}}

	return &Inventory{
		GroupOrder: []string{"all", "web", "cache"},
		Groups: map[string]Group{
			"all": {
				Name: "all",
				Vars: map[string]any{
					"env":    "prod",
					"nested": map[string]any{"a": 1},
				},
			},
			"web": {
				Name:  "web",
				Hosts: []Host{web01, shared01},
				Vars: map[string]any{
					"role":   "web",
					"nested": map[string]any{"b": 2},
				},
			},
			"cache": {
				Name:  "cache",
				Hosts: []Host{shared01},
				Vars: map[string]any{
					"role":   "cache",
					"nested": map[string]any{"c": 3},
				},
			},
		},
	}
}

// TestHostsForTarget_SingleGroup verifies the happy-path: a host in one group
// gets the expected merged vars.
func TestHostsForTarget_SingleGroup(t *testing.T) {
	inv := baseInventory()

	hosts, err := inv.HostsForTarget("web01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]

	// "all" group var
	if got := h.Vars["env"]; got != "prod" {
		t.Errorf("env: want %q, got %v", "prod", got)
	}
	// group var
	if got := h.Vars["role"]; got != "web" {
		t.Errorf("role: want %q, got %v", "web", got)
	}
	// host var
	if got := h.Vars["host_var"]; got != "web01_val" {
		t.Errorf("host_var: want %q, got %v", "web01_val", got)
	}
}

// TestHostsForTarget_MultiGroup is the regression test for bug #37.
// shared01 lives in both "web" and "cache"; it should receive vars from
// both groups, with the later group's scalar vars winning.
func TestHostsForTarget_MultiGroup(t *testing.T) {
	inv := baseInventory()

	hosts, err := inv.HostsForTarget("shared01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]

	// "all" group var is always present.
	if got := h.Vars["env"]; got != "prod" {
		t.Errorf("env: want %q, got %v", "prod", got)
	}

	// "cache" group comes after "web" in GroupOrder, so its scalar "role"
	// overwrites "web"'s.
	if got := h.Vars["role"]; got != "cache" {
		t.Errorf("role: want %q (last group wins), got %v", "cache", got)
	}

	// host var still wins over group vars.
	if got := h.Vars["host_var"]; got != "shared_val" {
		t.Errorf("host_var: want %q, got %v", "shared_val", got)
	}
}

// TestHostsForTarget_MultiGroup_DeepMerge is the regression test for bug #38.
// The "nested" key is a map in every layer; keys from all layers must survive,
// not be overwritten wholesale by a later layer's copy of the map.
func TestHostsForTarget_MultiGroup_DeepMerge(t *testing.T) {
	inv := baseInventory()

	hosts, err := inv.HostsForTarget("shared01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := hosts[0]

	nested, ok := h.Vars["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested is not map[string]any: %T", h.Vars["nested"])
	}

	// "a" comes from the "all" group.
	if got := nested["a"]; got != 1 {
		t.Errorf("nested.a: want 1, got %v", got)
	}
	// "b" comes from the "web" group.
	if got := nested["b"]; got != 2 {
		t.Errorf("nested.b: want 2, got %v", got)
	}
	// "c" comes from the "cache" group.
	if got := nested["c"]; got != 3 {
		t.Errorf("nested.c: want 3, got %v", got)
	}
}

// TestHostsForTarget_GroupLookup verifies that targeting a group name returns
// all hosts in that group with correct merged vars.
func TestHostsForTarget_GroupLookup(t *testing.T) {
	inv := baseInventory()

	hosts, err := inv.HostsForTarget("web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts in web group, got %d", len(hosts))
	}
}

// TestHostsForTarget_NotFound ensures a helpful error for unknown targets.
func TestHostsForTarget_NotFound(t *testing.T) {
	inv := baseInventory()

	_, err := inv.HostsForTarget("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
}

// TestAllHosts_Deduplication confirms that a host in multiple groups appears
// only once in AllHosts output.
func TestAllHosts_Deduplication(t *testing.T) {
	inv := baseInventory()

	hosts := inv.AllHosts()
	seen := map[string]int{}
	for _, h := range hosts {
		seen[h.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("host %q appears %d times in AllHosts, want 1", name, count)
		}
	}
}

// TestDeepMerge_Scalars verifies simple scalar overwrites.
func TestDeepMerge_Scalars(t *testing.T) {
	dst := map[string]any{"a": 1, "b": 2}
	src := map[string]any{"b": 99, "c": 3}
	maputil.DeepMerge(dst, src)

	if dst["a"] != 1 {
		t.Errorf("a: want 1, got %v", dst["a"])
	}
	if dst["b"] != 99 {
		t.Errorf("b: want 99, got %v", dst["b"])
	}
	if dst["c"] != 3 {
		t.Errorf("c: want 3, got %v", dst["c"])
	}
}

// TestDeepMerge_NestedMaps verifies that nested maps are merged recursively
// rather than replaced.
func TestDeepMerge_NestedMaps(t *testing.T) {
	dst := map[string]any{
		"settings": map[string]any{"timeout": 30, "retries": 3},
	}
	src := map[string]any{
		"settings": map[string]any{"retries": 5, "verbose": true},
	}
	maputil.DeepMerge(dst, src)

	settings, ok := dst["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings is not map[string]any: %T", dst["settings"])
	}
	if settings["timeout"] != 30 {
		t.Errorf("timeout: want 30, got %v", settings["timeout"])
	}
	if settings["retries"] != 5 {
		t.Errorf("retries: want 5, got %v", settings["retries"])
	}
	if settings["verbose"] != true {
		t.Errorf("verbose: want true, got %v", settings["verbose"])
	}
}

// TestDeepMerge_SrcMapOverwritesScalar verifies that a map in src replaces a
// scalar at the same key in dst (not the other way around).
func TestDeepMerge_SrcMapOverwritesScalar(t *testing.T) {
	dst := map[string]any{"key": "scalar"}
	src := map[string]any{"key": map[string]any{"nested": true}}
	maputil.DeepMerge(dst, src)

	m, ok := dst["key"].(map[string]any)
	if !ok {
		t.Fatalf("key should be a map after merge, got %T", dst["key"])
	}
	if m["nested"] != true {
		t.Errorf("nested: want true, got %v", m["nested"])
	}
}
