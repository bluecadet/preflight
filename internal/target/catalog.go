package target

// Capability describes which runtimes and environments a built-in module
// can execute in.
type Capability uint8

const (
	CapabilityInline         Capability = 1 << iota // usable as an inline YAML field in a task
	CapabilityRemote                                // usable over a remote transport (SSH, WinRM)
	CapabilityBuiltinCommon                         // available on any platform
	CapabilityBuiltinWindows                        // Windows-only
)

type catalogModule struct {
	Name       string
	Capability Capability
}

var catalogModules = []catalogModule{
	{Name: "registry", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "service", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "file", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "directory", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "package", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "shortcut", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "scheduled_task", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "user", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "winget_package", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "remove_appx_packages", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "power_plan", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "windows_feature", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "environment", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "firewall_rule", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "powershell", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "shell", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "reboot", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "wait", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
}

// CatalogNames returns module names whose capability includes the given mask.
func CatalogNames(cap Capability) []string {
	out := make([]string, 0, len(catalogModules))
	for _, m := range catalogModules {
		if m.Capability&cap != 0 {
			out = append(out, m.Name)
		}
	}
	return out
}

// CatalogSet returns a set of module names whose capability includes the given
// mask.
func CatalogSet(cap Capability) map[string]struct{} {
	set := make(map[string]struct{}, len(catalogModules))
	for _, name := range CatalogNames(cap) {
		set[name] = struct{}{}
	}
	return set
}
