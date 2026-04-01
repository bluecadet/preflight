//go:build windows

package module

import "github.com/bluecadet/preflight/internal/target"

// addWindowsModules registers Windows-native module implementations.
func addWindowsModules(reg target.ModuleRegistry) {
	reg["registry"] = &RegistryModule{}
	reg["service"] = &ServiceModule{}
	reg["package"] = &PackageModule{}
	reg["shortcut"] = &ShortcutModule{}
	reg["scheduled_task"] = &ScheduledTaskModule{}
	reg["user"] = &UserModule{}
	reg["winget_package"] = &WingetPackageModule{}
	reg["appx_package"] = &AppxPackageModule{}
	reg["power_plan"] = &PowerPlanModule{}
	reg["windows_feature"] = &WindowsFeatureModule{}
	reg["firewall_rule"] = &FirewallRuleModule{}
}
