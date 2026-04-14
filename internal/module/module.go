package module

import (
	"fmt"

	"github.com/bluecadet/preflight/internal/modulecatalog"
	"github.com/bluecadet/preflight/internal/target"
)

// Registry returns a map of all built-in module names to their implementations.
func Registry() target.ModuleRegistry {
	reg := make(target.ModuleRegistry, len(modulecatalog.Names(modulecatalog.CapabilityBuiltinCommon))+len(modulecatalog.Names(modulecatalog.CapabilityBuiltinWindows)))
	for _, name := range modulecatalog.Names(modulecatalog.CapabilityBuiltinCommon) {
		reg[name] = builtinCommonModule(name)
	}
	// Windows-only modules (stubs on non-Windows).
	addWindowsModules(reg)
	return reg
}

func builtinCommonModule(name string) target.Module {
	switch name {
	case "file":
		return &FileModule{}
	case "directory":
		return &DirectoryModule{}
	case "powershell":
		return &PowershellModule{}
	case "shell":
		return &ShellModule{}
	case "environment":
		return &EnvironmentModule{}
	case "wait":
		return &WaitModule{}
	case "reboot":
		return &RebootModule{}
	default:
		panic(fmt.Sprintf("unknown common builtin module %q", name))
	}
}
