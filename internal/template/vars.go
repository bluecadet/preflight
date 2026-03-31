// Package template provides Jinja-like template evaluation for preflight
// action and playbook YAML values.
package template

import "maps"

// VarLayer represents a precedence tier in the variable stack.
// Higher values win during merge.
type VarLayer int

const (
	LayerDefaults  VarLayer = iota // Built-in defaults
	LayerProject                   // preflight.yml project vars
	LayerGroupVars                 // Inventory group vars
	LayerHostVars                  // Inventory host vars
	LayerPlaybook                  // Playbook vars
	LayerCLI                       // --var CLI flags
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
		deepMerge(result, v.layers[i])
	}
	return result
}

// Get performs a key lookup across the merged result.
func (v *VarStore) Get(key string) (any, bool) {
	merged := v.Merge()
	val, ok := merged[key]
	return val, ok
}

// deepMerge merges src into dst in-place.
// When both dst[k] and src[k] are maps they are merged recursively.
// Otherwise src[k] overwrites dst[k].
func deepMerge(dst, src map[string]any) {
	for k, srcVal := range src {
		if srcMap, ok := toStringMap(srcVal); ok {
			if dstVal, exists := dst[k]; exists {
				if dstMap, ok := toStringMap(dstVal); ok {
					merged := make(map[string]any, len(dstMap))
					maps.Copy(merged, dstMap)
					deepMerge(merged, srcMap)
					dst[k] = merged
					continue
				}
			}
			// dst[k] is not a map — copy src map and use it
			cp := make(map[string]any, len(srcMap))
			maps.Copy(cp, srcMap)
			dst[k] = cp
		} else {
			dst[k] = srcVal
		}
	}
}

// toStringMap asserts that v is map[string]any.
func toStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}
