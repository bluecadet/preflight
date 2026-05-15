package module

import "github.com/bluecadet/preflight/internal/target"

// Registry returns a map of all built-in module names to their implementations.
func Registry() target.ModuleRegistry {
	reg := target.ModuleRegistry{
		"file":        &FileModule{},
		"directory":   &DirectoryModule{},
		"powershell":  &PowershellModule{},
		"shell":       &ShellModule{},
		"environment": &EnvironmentModule{},
		"wait":        &WaitModule{},
		"reboot":      &RebootModule{},
	}
	// Windows-only modules (stubs on non-Windows).
	addWindowsModules(reg)
	return reg
}
