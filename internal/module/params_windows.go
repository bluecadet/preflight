//go:build windows

package module

import "maps"

func cloneParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	cloned := make(map[string]any, len(params))
	maps.Copy(cloned, params)
	return cloned
}
