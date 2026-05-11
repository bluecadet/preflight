//go:build !windows

package winutil

// RefreshProcessPath is a no-op on non-Windows platforms.
func RefreshProcessPath() {}
