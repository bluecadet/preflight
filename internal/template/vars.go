// Package template provides Jinja-like template evaluation for preflight
// action and playbook YAML values.
package template

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
	layers [numLayers]map[string]interface{}
}

// NewVarStore returns an initialised VarStore with empty layers.
func NewVarStore() *VarStore {
	v := &VarStore{}
	for i := range v.layers {
		v.layers[i] = make(map[string]interface{})
	}
	return v
}

// Set stores a single key/value pair at the given layer.
func (v *VarStore) Set(layer VarLayer, key string, value interface{}) {
	v.layers[layer][key] = value
}

// SetMap merges all entries from m into the given layer.
func (v *VarStore) SetMap(layer VarLayer, m map[string]interface{}) {
	for k, val := range m {
		v.layers[layer][k] = val
	}
}

// Merge produces a single flat map by deep-merging all layers in order.
// Later layers (higher index) win for scalar values; nested maps are merged
// recursively rather than overwritten wholesale.
func (v *VarStore) Merge() map[string]interface{} {
	result := make(map[string]interface{})
	for i := 0; i < numLayers; i++ {
		deepMerge(result, v.layers[i])
	}
	return result
}

// Get performs a key lookup across the merged result.
func (v *VarStore) Get(key string) (interface{}, bool) {
	merged := v.Merge()
	val, ok := merged[key]
	return val, ok
}

// deepMerge merges src into dst in-place.
// When both dst[k] and src[k] are maps they are merged recursively.
// Otherwise src[k] overwrites dst[k].
func deepMerge(dst, src map[string]interface{}) {
	for k, srcVal := range src {
		if srcMap, ok := toStringMap(srcVal); ok {
			if dstVal, exists := dst[k]; exists {
				if dstMap, ok := toStringMap(dstVal); ok {
					merged := make(map[string]interface{}, len(dstMap))
					for dk, dv := range dstMap {
						merged[dk] = dv
					}
					deepMerge(merged, srcMap)
					dst[k] = merged
					continue
				}
			}
			// dst[k] is not a map — copy src map and use it
			cp := make(map[string]interface{}, len(srcMap))
			for sk, sv := range srcMap {
				cp[sk] = sv
			}
			dst[k] = cp
		} else {
			dst[k] = srcVal
		}
	}
}

// toStringMap asserts that v is map[string]interface{}.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}
