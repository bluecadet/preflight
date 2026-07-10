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

func TestCatalogSupportedRuntimes_UnknownModuleReturnsNil(t *testing.T) {
	if got := CatalogSupportedRuntimes("does_not_exist"); got != nil {
		t.Errorf("expected nil for unknown module, got %v", got)
	}
	if CatalogKnownModule("does_not_exist") {
		t.Error("expected CatalogKnownModule=false for unknown module")
	}
}

// TestCatalogEnvironmentAndRebootAreWindowsOnly guards the drift fix: these
// were BuiltinCommon but are not implemented on POSIX (reboot stubs error,
// environment is a stated POSIX limitation). The catalog must reflect reality.
func TestCatalogEnvironmentAndRebootAreWindowsOnly(t *testing.T) {
	for _, name := range []string{"environment", "reboot"} {
		if CatalogSupportsRuntime(name, RuntimeKindPOSIXShell) {
			t.Errorf("module %q must not be marked supported on posix-shell", name)
		}
		if !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("module %q should be supported on windows-powershell", name)
		}
	}
}

func TestCatalogMatrixIsPartitioned(t *testing.T) {
	// Every catalog module must be supported on windows-powershell (windows is
	// the superset). Only BuiltinCommon modules are additionally supported on
	// posix-shell.
	for _, name := range CatalogNames(CapabilityRemote) {
		if !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("module %q is not supported on windows-powershell (windows is the baseline)", name)
		}
	}
}
