package pscript

import (
	"strings"
	"testing"
)

func TestRegistryScriptsSupportBinaryPatch(t *testing.T) {
	for name, script := range map[string]string{
		"check":  RegistryCheckScript,
		"apply":  RegistryApplyScript,
		"ensure": RegistryEnsureScript,
	} {
		t.Run(name, func(t *testing.T) {
			for _, fragment := range []string{
				"$spec.patch",
			} {
				if !strings.Contains(script, fragment) {
					t.Fatalf("expected %s script to contain %q", name, fragment)
				}
			}
		})
	}
	for name, script := range map[string]string{
		"check":  RegistryCheckScript,
		"ensure": RegistryEnsureScript,
	} {
		t.Run(name+" check", func(t *testing.T) {
			if !strings.Contains(script, "registry: patch is only supported for binary values") {
				t.Fatalf("expected %s script to validate binary patches", name)
			}
		})
	}
	for name, script := range map[string]string{
		"apply":  RegistryApplyScript,
		"ensure": RegistryEnsureScript,
	} {
		t.Run(name+" apply", func(t *testing.T) {
			if !strings.Contains(script, "binary patch offset") {
				t.Fatalf("expected %s script to guard patch offsets", name)
			}
		})
	}
}
