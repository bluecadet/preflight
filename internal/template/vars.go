// Package template provides Jinja-like template evaluation for preflight
// action and playbook YAML values.
package template

import (
	"maps"

	"github.com/bluecadet/preflight/internal/maputil"
)

// VarLayer represents a precedence tier in the variable stack.
// Higher values win during merge.
type VarLayer int

const (
	LayerDefaults      VarLayer = iota // reserved for future built-in defaults
	LayerProject                       // preflight.yml project vars
	LayerInventoryVars                 // Inventory vars (group+host already merged before reaching the runner)
	LayerHostVars                      // reserved; inventory pre-merges host+group vars before they reach the runner
	LayerPlaybook                      // Playbook vars
	LayerCLI                           // --var CLI flags
)

const numLayers = 6

// VarStore holds variables across the six precedence layers.
// Later layers (higher VarLayer value) win during merge.
type VarStore struct {
	layers [numLayers]map[string]any
}

// NewVarStore returns an initialised VarStore with empty layers.
func NewVarStore() *VarStore {
	v := &VarStore{}
	for i := range v.layers {
		v.layers[i] = make(map[string]any)
	}
	return v
}

// Set stores a single key/value pair at the given layer.
func (v *VarStore) Set(layer VarLayer, key string, value any) {
	v.layers[layer][key] = value
}

// SetMap merges all entries from m into the given layer.
func (v *VarStore) SetMap(layer VarLayer, m map[string]any) {
	maps.Copy(v.layers[layer], m)
}

// Merge produces a single flat map by deep-merging all layers in order.
// Later layers (higher index) win for scalar values; nested maps are merged
// recursively rather than overwritten wholesale.
func (v *VarStore) Merge() map[string]any {
	result := make(map[string]any)
	for i := range numLayers {
		maputil.DeepMerge(result, v.layers[i])
	}
	return result
}

// Get performs a key lookup across the merged result.
func (v *VarStore) Get(key string) (any, bool) {
	merged := v.Merge()
	val, ok := merged[key]
	return val, ok
}
