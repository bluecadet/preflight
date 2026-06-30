package module

// WindowsModuleNames is the canonical list of Windows-only module names.
// It serves as the single source of truth for both the Windows native
// registrations (module_windows.go) and the cross-platform stubs
// (windows_stub.go). Add a new name here and a corresponding entry in
// windowsModuleTypes (in module_windows.go) to register a new module.
var WindowsModuleNames = []string{
	"registry",
	"service",
	"package",
	"shortcut",
	"scheduled_task",
	"user",
	"winget_package",
	"remove_appx_packages",
	"power_plan",
	"windows_feature",
	"firewall_rule",
}
