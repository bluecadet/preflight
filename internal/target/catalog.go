package target

import "slices"

// catalogModules is the single hand-written list of built-in module names.
// Everything else (registries, inline YAML keys, schemas) is derived from it
// or asserted against it in tests.
var catalogModules = []string{
	"registry",
	"service",
	"file",
	"directory",
	"package",
	"shortcut",
	"scheduled_task",
	"user",
	"winget_package",
	"choco_package",
	"scoop_package",
	"remove_appx_packages",
	"power_plan",
	"windows_feature",
	"environment",
	"firewall_rule",
	"powershell",
	"shell",
	"reboot",
	"wait",
}

// CatalogNames returns the built-in module names in catalog order.
func CatalogNames() []string {
	return slices.Clone(catalogModules)
}

// CatalogKnownModule reports whether name is a catalog built-in module.
func CatalogKnownModule(name string) bool {
	return slices.Contains(catalogModules, name)
}
