//go:build !windows

package module_test

import (
	"context"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/module"
)

func TestWindowsOnlyModulesUseNonWindowsStubs(t *testing.T) {
	reg := module.Registry()
	for _, name := range []string{
		"registry",
		"service",
		"package",
		"shortcut",
		"scheduled_task",
		"user",
		"windows_feature",
		"firewall_rule",
	} {
		mod, ok := reg[name]
		if !ok {
			t.Fatalf("expected module %q in registry", name)
		}
		_, err := mod.Check(context.Background(), map[string]any{})
		if err == nil {
			t.Fatalf("expected non-Windows stub error for %q", name)
		}
		if !strings.Contains(err.Error(), "only supported on Windows") {
			t.Fatalf("expected Windows-only error for %q, got %v", name, err)
		}
	}
}
