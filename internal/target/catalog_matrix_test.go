package target

import (
	"testing"
)

func TestCatalogSupportedRuntimes_CommonModulesRunOnBoth(t *testing.T) {
	for _, name := range CatalogNames(CapabilityBuiltinCommon) {
		got := CatalogSupportedRuntimes(name)
		if len(got) != 2 {
			t.Errorf("common module %q: expected 2 runtimes, got %v", name, got)
		}
		if !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("common module %q should support windows-powershell", name)
		}
		if !CatalogSupportsRuntime(name, RuntimeKindPOSIXShell) {
			t.Errorf("common module %q should support posix-shell", name)
		}
	}
}

func TestCatalogSupportedRuntimes_WindowsModulesAreWindowsOnly(t *testing.T) {
	for _, name := range CatalogNames(CapabilityBuiltinWindows) {
		if CatalogSupportsRuntime(name, RuntimeKindPOSIXShell) {
			t.Errorf("windows module %q must not be supported on posix-shell", name)
		}
		if !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("windows module %q should support windows-powershell", name)
		}
	}
}

// TestCatalogSupportedRuntimes_POSIXModulesArePOSIXOnly guards the
// POSIX-only capability: a module marked BuiltinPOSIX runs on posix-shell
// and nothing else. system_package is the first such module.
func TestCatalogSupportedRuntimes_POSIXModulesArePOSIXOnly(t *testing.T) {
	for _, name := range CatalogNames(CapabilityBuiltinPOSIX) {
		if !CatalogSupportsRuntime(name, RuntimeKindPOSIXShell) {
			t.Errorf("posix module %q should support posix-shell", name)
		}
		if CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("posix module %q must not be supported on windows-powershell", name)
		}
	}
}

func TestCatalogSupportedRuntimes_UnknownModuleReturnsNil(t *testing.T) {
	if got := CatalogSupportedRuntimes("does_not_exist"); got != nil {
		t.Errorf("expected nil for unknown module, got %v", got)
	}
	if CatalogKnownModule("does_not_exist") {
		t.Error("expected CatalogKnownModule=false for unknown module")
	}
}

// TestCatalogEnvironmentIsWindowsOnly guards the drift fix: environment is a
// stated POSIX limitation (ambient env is login-shell plumbing with no faithful
// analog) and must not be marked supported on posix-shell. reboot is now
// cross-platform (BuiltinCommon) with a POSIX implementation.
func TestCatalogEnvironmentIsWindowsOnly(t *testing.T) {
	for _, name := range []string{"environment"} {
		if CatalogSupportsRuntime(name, RuntimeKindPOSIXShell) {
			t.Errorf("module %q must not be marked supported on posix-shell", name)
		}
		if !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("module %q should be supported on windows-powershell", name)
		}
	}
}

func TestCatalogMatrixIsPartitioned(t *testing.T) {
	// Every remote module supports at least one runtime, and its builtin
	// capability grants exactly one of: both runtimes (BuiltinCommon),
	// windows-powershell only (BuiltinWindows), or posix-shell only
	// (BuiltinPOSIX). Any other combination is an inconsistent partition.
	for _, name := range CatalogNames(CapabilityRemote) {
		runtimes := CatalogSupportedRuntimes(name)
		if len(runtimes) == 0 {
			t.Errorf("module %q is not supported on any runtime (orphan)", name)
		}
		common := CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) &&
			CatalogSupportsRuntime(name, RuntimeKindPOSIXShell)
		winOnly := CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) &&
			!CatalogSupportsRuntime(name, RuntimeKindPOSIXShell)
		posixOnly := !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) &&
			CatalogSupportsRuntime(name, RuntimeKindPOSIXShell)
		if !common && !winOnly && !posixOnly {
			t.Errorf("module %q has an inconsistent runtime partition: %v", name, runtimes)
		}
	}
}

// TestCatalogRequiresRoot guards the requires_root flag: the modules that need
// root on POSIX are marked, and no module is marked by accident.
func TestCatalogRequiresRoot(t *testing.T) {
	for _, name := range []string{"service", "user", "reboot", "system_package"} {
		if !CatalogRequiresRoot(name) {
			t.Errorf("module %q should be marked requires_root", name)
		}
	}
	// A sample of modules that must NOT carry the flag.
	for _, name := range []string{"file", "directory", "shell", "wait", "powershell", "registry", "package"} {
		if CatalogRequiresRoot(name) {
			t.Errorf("module %q must not be marked requires_root", name)
		}
	}
}

// TestCatalogRequiresRootUnknownModuleIsFalse guards the accessor's unknown-name
// behavior: an unknown module is not root-requiring (it is unknown, handled
// elsewhere).
func TestCatalogRequiresRootUnknownModuleIsFalse(t *testing.T) {
	if CatalogRequiresRoot("does_not_exist") {
		t.Error("expected false for unknown module")
	}
}
