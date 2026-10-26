package pscript

import (
	"strings"
	"testing"
)

func TestUserProfileHelpersLoadAndUnloadHive(t *testing.T) {
	for _, fragment := range []string{
		`'HKU\' + $mount`,
		"reg.exe load",
		"reg.exe unload",
		"[gc]::Collect()",
		"has no profile on this machine; sign in once to create it",
		"finally",
		"if ($bodyFailed) { Write-Warning $unloadErr }",
	} {
		if !strings.Contains(UserProfileHelpers, fragment) {
			t.Errorf("UserProfileHelpers missing %q", fragment)
		}
	}
}

func TestUserScopedScriptsUseProfileHelper(t *testing.T) {
	for name, script := range map[string]string{
		"registry check": RegistryCheckScript,
		"registry apply": RegistryApplyScript,
		"env check":      EnvironmentUserCheckScript,
		"env apply":      EnvironmentUserApplyScript,
	} {
		if !strings.Contains(script, "Invoke-WithUserHive") {
			t.Errorf("%s script does not use Invoke-WithUserHive", name)
		}
		if strings.Contains(script, "exit 0") && strings.HasPrefix(name, "registry") {
			t.Errorf("%s script must return, not exit, so the hive is unloaded", name)
		}
	}
}
