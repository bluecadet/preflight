package module

import "github.com/bluecadet/preflight/internal/target"

// PreservesSecretRefs reports whether the named module requires template
// rendering to preserve secret refs rather than resolving them eagerly.
// The file module defers secret resolution because content_template is
// bound as a late step after main param rendering.
func PreservesSecretRefs(name string) bool {
	return name == "file"
}

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
