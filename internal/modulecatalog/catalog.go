package modulecatalog

type Capability uint8

const (
	CapabilityInline Capability = 1 << iota
	CapabilityRemote
	CapabilityBuiltinCommon
	CapabilityBuiltinWindows
)

type Module struct {
	Name       string
	Capability Capability
}

var modules = []Module{
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

func Names(cap Capability) []string {
	out := make([]string, 0, len(modules))
	for _, module := range modules {
		if module.Capability&cap != 0 {
			out = append(out, module.Name)
		}
	}
	return out
}

func Set(cap Capability) map[string]struct{} {
	set := make(map[string]struct{}, len(modules))
	for _, name := range Names(cap) {
		set[name] = struct{}{}
	}
	return set
}
