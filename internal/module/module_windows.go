//go:build windows

package module

import (
	"fmt"
	"slices"

	"github.com/bluecadet/preflight/internal/target"
)

// windowsModuleTypes maps each Windows module name to its implementation.
// Add entries here when adding a new Windows module.
var windowsModuleTypes = map[string]target.Module{
	"registry":             &RegistryModule{},
	"service":              &ServiceModule{},
	"package":              &PackageModule{},
	"shortcut":             &ShortcutModule{},
	"scheduled_task":       &ScheduledTaskModule{},
	"user":                 &UserModule{},
	"winget_package":       &WingetPackageModule{},
	"remove_appx_packages": &RemoveAppxPackagesModule{},
	"power_plan":           &PowerPlanModule{},
	"windows_feature":      &WindowsFeatureModule{},
	"firewall_rule":        &FirewallRuleModule{},
}

func init() {
	// Cross-check: every name in WindowsModuleNames must have a registered type,
	// and every registered type must have a corresponding name.
	for _, name := range WindowsModuleNames {
		if _, ok := windowsModuleTypes[name]; !ok {
			panic(fmt.Sprintf("module %q in WindowsModuleNames has no type in windowsModuleTypes", name))
		}
	}
	for name := range windowsModuleTypes {
		if !slices.Contains(WindowsModuleNames, name) {
			panic(fmt.Sprintf("module %q in windowsModuleTypes has no entry in WindowsModuleNames", name))
		}
	}
}

// addWindowsModules registers Windows-native module implementations.
func addWindowsModules(reg target.ModuleRegistry) {
	for name, mod := range windowsModuleTypes {
		reg[name] = mod
	}
}
