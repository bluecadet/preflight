package template

import (
	"testing"
)

func TestVarStore_CLIOverridesProject(t *testing.T) {
	s := NewVarStore()
	s.Set(LayerProject, "key", "project-value")
	s.Set(LayerCLI, "key", "cli-value")

	merged := s.Merge()
	if merged["key"] != "cli-value" {
		t.Errorf("expected cli-value, got %v", merged["key"])
	}
}

func TestVarStore_DeepMergeNestedMaps(t *testing.T) {
	s := NewVarStore()

	// Inventory vars (pre-merged group+host) set a nested map with two keys
	s.Set(LayerInventoryVars, "db", map[string]any{
		"host": "db.internal",
		"port": 5432,
	})
	// A subsequent layer (e.g. host-level override) overrides only the port
	s.Set(LayerHostVars, "db", map[string]any{
		"port": 5433,
	})

	merged := s.Merge()
	db, ok := merged["db"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any for db, got %T", merged["db"])
	}
	if db["host"] != "db.internal" {
		t.Errorf("host: expected db.internal, got %v", db["host"])
	}
	if db["port"] != 5433 {
		t.Errorf("port: expected 5433, got %v", db["port"])
	}
}

func TestVarStore_MergeOrder(t *testing.T) {
	s := NewVarStore()
	s.Set(LayerDefaults, "x", "defaults")
	s.Set(LayerProject, "x", "project")
	s.Set(LayerInventoryVars, "x", "group")
	s.Set(LayerHostVars, "x", "host")
	s.Set(LayerPlaybook, "x", "playbook")
	s.Set(LayerCLI, "x", "cli")

	merged := s.Merge()
	if merged["x"] != "cli" {
		t.Errorf("expected cli (highest precedence), got %v", merged["x"])
	}
}

func TestVarStore_SetMap(t *testing.T) {
	s := NewVarStore()
	s.SetMap(LayerPlaybook, map[string]any{
		"a": 1,
		"b": "two",
	})

	merged := s.Merge()
	if merged["a"] != 1 {
		t.Errorf("a: expected 1, got %v", merged["a"])
	}
	if merged["b"] != "two" {
		t.Errorf("b: expected two, got %v", merged["b"])
	}
}

func TestVarStore_Get(t *testing.T) {
	s := NewVarStore()
	s.Set(LayerProject, "greeting", "hello")

	val, ok := s.Get("greeting")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "hello" {
		t.Errorf("expected hello, got %v", val)
	}
}

func TestVarStore_GetMissing(t *testing.T) {
	s := NewVarStore()
	_, ok := s.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for missing key")
	}
}

func TestVarStore_EmptyLayers(t *testing.T) {
	s := NewVarStore()
	merged := s.Merge()
	if len(merged) != 0 {
		t.Errorf("expected empty merge, got %v", merged)
	}
}

// TestVarStore_ScalarOverwritesMap verifies that when a higher-priority layer
// sets a scalar for a key that a lower layer set as a map, the scalar wins.
func TestVarStore_ScalarOverwritesMap(t *testing.T) {
	s := NewVarStore()
	s.Set(LayerDefaults, "val", map[string]any{"nested": "x"})
	s.Set(LayerCLI, "val", "scalar")

	merged := s.Merge()
	if merged["val"] != "scalar" {
		t.Errorf("expected scalar, got %v", merged["val"])
	}
}
