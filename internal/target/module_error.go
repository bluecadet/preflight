package target

import (
	"errors"
	"fmt"
	"strings"
)

// ModuleSupportClass enumerates the reasons a module cannot run on a target.
// Every transport surfaces the same set so error wording and run-log reason
// codes stay uniform across local, SSH, and WinRM.
type ModuleSupportClass string

const (
	// ClassUnknownModule: the module name is neither a catalog built-in nor a
	// discovered plugin.
	ClassUnknownModule ModuleSupportClass = "unknown_module"
	// ClassUnsupportedOnRuntime: a catalog built-in that is not supported on
	// the target's runtime. SupportedRuntimes lists where it does run.
	ClassUnsupportedOnRuntime ModuleSupportClass = "unsupported_on_runtime"
	// ClassMissingPrerequisite: the module is supported on this runtime in
	// principle but an environment prerequisite is absent (e.g. no pwsh
	// binary for the powershell module on posix-shell). Detail names it.
	ClassMissingPrerequisite ModuleSupportClass = "missing_prerequisite"
	// ClassPluginBecome: a plugin module was invoked with become enabled;
	// plugin+become is refused.
	ClassPluginBecome ModuleSupportClass = "plugin_become"
	// ClassPluginProtocol: a plugin failed the protocol handshake (version
	// mismatch / pre-v1 plugin rejected).
	ClassPluginProtocol ModuleSupportClass = "plugin_protocol"
)

// ModuleSupportError is the single typed error for module-by-runtime gaps.
// It carries the module name, the target's runtime kind (when known), and the
// runtimes that do support the module (for the unsupported class). Every
// transport constructs and renders it the same way.
type ModuleSupportError struct {
	Class             ModuleSupportClass
	Module            string
	RuntimeKind       RuntimeKind
	SupportedRuntimes []RuntimeKind
	Detail            string
}

// NewUnknownModuleError constructs an unknown_module error. RuntimeKind is
// unknown because the module was never recognized.
func NewUnknownModuleError(module string) *ModuleSupportError {
	return &ModuleSupportError{Class: ClassUnknownModule, Module: module}
}

// NewUnsupportedOnRuntimeError constructs an unsupported_on_runtime error for
// a catalog built-in, deriving the supporting runtimes from the catalog.
func NewUnsupportedOnRuntimeError(module string, kind RuntimeKind) *ModuleSupportError {
	return &ModuleSupportError{
		Class:             ClassUnsupportedOnRuntime,
		Module:            module,
		RuntimeKind:       kind,
		SupportedRuntimes: CatalogSupportedRuntimes(module),
	}
}

// NewMissingPrerequisiteError constructs a missing_prerequisite error.
func NewMissingPrerequisiteError(module string, kind RuntimeKind, detail string) *ModuleSupportError {
	return &ModuleSupportError{
		Class:       ClassMissingPrerequisite,
		Module:      module,
		RuntimeKind: kind,
		Detail:      detail,
	}
}

// NewPluginBecomeError constructs a plugin_become error.
func NewPluginBecomeError(module string) *ModuleSupportError {
	return &ModuleSupportError{Class: ClassPluginBecome, Module: module}
}

// NewPluginProtocolError constructs a plugin_protocol error.
func NewPluginProtocolError(module string, detail string) *ModuleSupportError {
	return &ModuleSupportError{Class: ClassPluginProtocol, Module: module, Detail: detail}
}

// ReasonCode returns the stable run-log reason code for this error.
func (e *ModuleSupportError) ReasonCode() string { return string(e.Class) }

// Error renders a uniform, prose-free message. Every class names the module;
// unsupported_on_runtime also names the target runtime and the supporting
// runtimes. No did-you-mean, no remediation.
func (e *ModuleSupportError) Error() string {
	switch e.Class {
	case ClassUnknownModule:
		return fmt.Sprintf("module %q is not a known module", e.Module)
	case ClassUnsupportedOnRuntime:
		if len(e.SupportedRuntimes) > 0 {
			return fmt.Sprintf("module %q is not supported on %s (supported: %s)",
				e.Module, e.RuntimeKind, joinRuntimeKinds(e.SupportedRuntimes))
		}
		return fmt.Sprintf("module %q is not supported on %s", e.Module, e.RuntimeKind)
	case ClassMissingPrerequisite:
		return fmt.Sprintf("module %q is missing a prerequisite on %s: %s",
			e.Module, e.RuntimeKind, e.Detail)
	case ClassPluginBecome:
		return fmt.Sprintf("plugin module %q does not support become", e.Module)
	case ClassPluginProtocol:
		return fmt.Sprintf("plugin module %q: %s", e.Module, e.Detail)
	default:
		return fmt.Sprintf("module %q: %s", e.Module, e.Class)
	}
}

// joinRuntimeKinds renders a comma-separated list of runtime kinds.
func joinRuntimeKinds(kinds []RuntimeKind) string {
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}

// classifyMissingModule classifies a module that a remote runtime registry
// could not resolve, using the controller registry to distinguish a recognized
// plugin from a genuinely unknown name. Every catalog built-in is present in
// a remote runtime registry (as a real impl or an unsupported stub), so a
// lookup miss is always one of these two cases: a plugin discovered on the
// controller (recognized but not runnable over this transport yet) surfaces
// unsupported_on_runtime; an unknown name surfaces unknown_module. This is the
// runtime-side counterpart to ValidateModuleForPlan, used by every transport's
// executeModule unsupported callback.
func classifyMissingModule(controllerRegistry ModuleRegistry, module string, kind RuntimeKind) error {
	if controllerRegistry == nil {
		return NewUnknownModuleError(module)
	}
	if _, ok := controllerRegistry[module]; !ok {
		return NewUnknownModuleError(module)
	}
	return NewUnsupportedOnRuntimeError(module, kind)
}

// ReasonCodeForError extracts a stable reason code from an error chain. It
// recognizes *ModuleSupportError; other errors return the empty string so
// callers can leave the reason field absent for untyped failures.
func ReasonCodeForError(err error) string {
	var mse *ModuleSupportError
	if errors.As(err, &mse) {
		return mse.ReasonCode()
	}
	return ""
}
