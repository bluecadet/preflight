//go:build windows

package module

import "github.com/bluecadet/preflight/internal/target"

// addWindowsModules registers Windows-native module implementations.
// Windows-only modules (registry, service, etc.) are added here as they are implemented.
func addWindowsModules(reg target.ModuleRegistry) {
	// TODO: register implementations as Windows-native modules are built
	_ = reg
}
