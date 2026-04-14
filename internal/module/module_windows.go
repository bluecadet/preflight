//go:build windows

package module

import (
	"fmt"

	"github.com/bluecadet/preflight/internal/modulecatalog"
	"github.com/bluecadet/preflight/internal/target"
)

// addWindowsModules registers Windows-native module implementations.
func addWindowsModules(reg target.ModuleRegistry) {
	for _, name := range modulecatalog.Names(modulecatalog.CapabilityBuiltinWindows) {
		reg[name] = builtinWindowsModule(name)
	}
}

func builtinWindowsModule(name string) target.Module {
	switch name {
	case "registry":
		return &RegistryModule{}
	case "service":
		return &ServiceModule{}
	case "package":
		return &PackageModule{}
	case "shortcut":
		return &ShortcutModule{}
	case "scheduled_task":
		return &ScheduledTaskModule{}
	case "user":
		return &UserModule{}
	case "winget_package":
		return &WingetPackageModule{}
	case "remove_appx_packages":
		return &RemoveAppxPackagesModule{}
	case "power_plan":
		return &PowerPlanModule{}
	case "windows_feature":
		return &WindowsFeatureModule{}
	case "firewall_rule":
		return &FirewallRuleModule{}
	default:
		panic(fmt.Sprintf("unknown windows builtin module %q", name))
	}
}
