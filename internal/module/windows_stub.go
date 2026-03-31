//go:build !windows

package module

import (
	"context"
	"fmt"

	"github.com/claytercek/preflight/internal/target"
)

func addWindowsModules(reg target.ModuleRegistry) {
	for _, name := range []string{
		"registry", "service", "package", "shortcut",
		"scheduled_task", "user", "windows_feature", "firewall_rule",
	} {
		n := name
		reg[n] = &windowsStubModule{name: n}
	}
}

type windowsStubModule struct{ name string }

func (m *windowsStubModule) Check(_ context.Context, _ map[string]interface{}) (bool, error) {
	return false, fmt.Errorf("module %q is only supported on Windows", m.name)
}

func (m *windowsStubModule) Apply(_ context.Context, _ map[string]interface{}) error {
	return fmt.Errorf("module %q is only supported on Windows", m.name)
}
