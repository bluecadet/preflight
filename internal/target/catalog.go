package target

import "slices"

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
	Name         string
	Capability   Capability
	RequiresRoot bool // POSIX: module needs an effective root user (run as root or via become to root)
}

var catalogModules = []catalogModule{
	{Name: "registry", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "service", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows, RequiresRoot: true},
	{Name: "file", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "directory", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "package", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "shortcut", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "scheduled_task", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "user", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon, RequiresRoot: true},
	{Name: "winget_package", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "remove_appx_packages", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "power_plan", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "windows_feature", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "environment", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "firewall_rule", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinWindows},
	{Name: "powershell", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "shell", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon},
	{Name: "reboot", Capability: CapabilityInline | CapabilityRemote | CapabilityBuiltinCommon, RequiresRoot: true},
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

// CatalogKnownModule reports whether name is a catalog built-in module.
func CatalogKnownModule(name string) bool {
	_, ok := knownRemoteModuleSet[name]
	return ok
}

// CatalogSupportedRuntimes returns the runtime kinds the catalog marks as
// supporting the named module. BuiltinCommon modules run on both
// windows-powershell and posix-shell; BuiltinWindows modules run on
// windows-powershell only. An unknown module returns nil.
func CatalogSupportedRuntimes(name string) []RuntimeKind {
	var cap Capability
	for _, m := range catalogModules {
		if m.Name == name {
			cap = m.Capability
			break
		}
	}
	if cap == 0 {
		return nil
	}
	var runtimes []RuntimeKind
	if cap&CapabilityBuiltinCommon != 0 {
		runtimes = append(runtimes, RuntimeKindWindowsPowerShell, RuntimeKindPOSIXShell)
	} else if cap&CapabilityBuiltinWindows != 0 {
		runtimes = append(runtimes, RuntimeKindWindowsPowerShell)
	}
	return runtimes
}

// CatalogSupportsRuntime reports whether the catalog marks name as supported
// on the given runtime kind.
func CatalogSupportsRuntime(name string, kind RuntimeKind) bool {
	return slices.Contains(CatalogSupportedRuntimes(name), kind)
}

// CatalogRequiresRoot reports whether the catalog marks name as requiring an
// effective root user on POSIX. Root-requiring modules fail before Check()
// when the effective execution user is not root — enforced by the shared
// execution layer, not by individual modules. An unknown module returns
// false (it is handled as unknown elsewhere).
func CatalogRequiresRoot(name string) bool {
	for _, m := range catalogModules {
		if m.Name == name {
			return m.RequiresRoot
		}
	}
	return false
}
