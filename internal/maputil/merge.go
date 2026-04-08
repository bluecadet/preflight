package maputil

import "maps"

// DeepMerge merges src into dst in-place. When both dst[k] and src[k] are
// maps they are merged recursively; otherwise src[k] overwrites dst[k].
func DeepMerge(dst, src map[string]any) {
	for k, srcVal := range src {
		if srcMap, ok := toStringMap(srcVal); ok {
			if dstVal, exists := dst[k]; exists {
				if dstMap, ok := toStringMap(dstVal); ok {
					merged := make(map[string]any, len(dstMap))
					maps.Copy(merged, dstMap)
					DeepMerge(merged, srcMap)
					dst[k] = merged
					continue
				}
			}
			cp := make(map[string]any, len(srcMap))
			maps.Copy(cp, srcMap)
			dst[k] = cp
		} else {
			dst[k] = srcVal
		}
	}
}

func toStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}
