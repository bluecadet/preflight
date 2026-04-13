package winutil

// CloneParams returns a shallow copy of params.
func CloneParams(params map[string]any) map[string]any {
	return cloneParams(params)
}
