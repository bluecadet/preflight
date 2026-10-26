package target

import (
	"testing"
)

func TestCatalogSupportedRuntimes_EveryModuleRunsOnWindows(t *testing.T) {
	for _, name := range CatalogNames(CapabilityRemote) {
		got := CatalogSupportedRuntimes(name)
		if len(got) != 1 || got[0] != RuntimeKindWindowsPowerShell {
			t.Errorf("module %q: expected only windows-powershell, got %v", name, got)
		}
		if !CatalogSupportsRuntime(name, RuntimeKindWindowsPowerShell) {
			t.Errorf("module %q should support windows-powershell", name)
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
