package target

import (
	"fmt"
	"runtime"
)

// RuntimeKind derives the module runtime used by a declared destination
// platform. Inventory schema validation keeps declarations within the known
// OS families; false protects programmatic callers from an unknown value.
func (p Platform) RuntimeKind() (RuntimeKind, bool) {
	switch p.OS {
	case OSFamilyWindows:
		return RuntimeKindWindowsPowerShell, true
	default:
		return "", false
	}
}

// PlanRuntimeForTransport returns the runtime kind a transport implies at plan
// time, before any remote probe. WinRM is always windows-powershell and local
// is windows-powershell on a Windows controller; both are knowable offline.
// Local on any other controller OS is an error. SSH returns ok=false because
// its runtime is only known after probing the remote host — plan-time can only
// name-check SSH tasks.
func PlanRuntimeForTransport(t Transport) (RuntimeKind, bool, error) {
	switch t {
	case TransportWinRM:
		return RuntimeKindWindowsPowerShell, true, nil
	case TransportLocal:
		if runtime.GOOS != "windows" {
			return "", false, errLocalRequiresWindows()
		}
		return RuntimeKindWindowsPowerShell, true, nil
	default:
		return "", false, nil
	}
}

func errLocalRequiresWindows() error {
	return fmt.Errorf("local target is only supported on Windows (controller OS is %s); use an ssh or winrm target", runtime.GOOS)
}

// IsKnownModule reports whether name is a catalog built-in or present in the
// controller registry (i.e. a discovered plugin). It is the plan-time
// name-check shared by every transport.
func IsKnownModule(name string, controllerRegistry ModuleRegistry) bool {
	if CatalogKnownModule(name) {
		return true
	}
	if controllerRegistry != nil {
		if _, ok := controllerRegistry[name]; ok {
			return true
		}
	}
	return false
}

// IsPluginModule reports whether name is a discovered plugin in the controller
// registry. Plugins bypass the runtime support matrix: controller-side
// execution makes them supported on every runtime.
func IsPluginModule(name string, controllerRegistry ModuleRegistry) bool {
	if controllerRegistry == nil {
		return false
	}
	mod, ok := controllerRegistry[name]
	if !ok {
		return false
	}
	_, isPlugin := mod.(PluggableModule)
	return isPlugin
}

// ValidateModuleForPlan checks a module name against the catalog matrix and
// controller registry at plan time. It returns a *ModuleSupportError when the
// module is unknown or (for transports with a knowable runtime) unsupported on
// the target's runtime. Plugins bypass the runtime check.
//
// When kindKnown is false (SSH), only the unknown-module name-check runs.
func ValidateModuleForPlan(module string, kind RuntimeKind, kindKnown bool, controllerRegistry ModuleRegistry) error {
	if !IsKnownModule(module, controllerRegistry) {
		return NewUnknownModuleError(module)
	}
	if !kindKnown {
		return nil
	}
	if IsPluginModule(module, controllerRegistry) {
		return nil
	}
	if !CatalogSupportsRuntime(module, kind) {
		return NewUnsupportedOnRuntimeError(module, kind)
	}
	return nil
}
