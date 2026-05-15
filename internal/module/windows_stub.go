//go:build !windows

package module

import (
	"context"
	"errors"

	"github.com/bluecadet/preflight/internal/preflighterr"
	"github.com/bluecadet/preflight/internal/target"
)

var windowsModuleNames = []string{
	"registry",
	"service",
	"package",
	"shortcut",
	"scheduled_task",
	"user",
	"winget_package",
	"remove_appx_packages",
	"power_plan",
	"windows_feature",
	"firewall_rule",
}

func addWindowsModules(reg target.ModuleRegistry) {
	for _, name := range windowsModuleNames {
		n := name
		reg[n] = &windowsStubModule{name: n}
	}
}

type windowsStubModule struct{ name string }

func (m *windowsStubModule) Check(_ context.Context, _ map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	return target.CheckResult{}, &preflighterr.ModuleError{Module: m.name, Op: "check", Err: errors.New("only supported on Windows")}
}

func (m *windowsStubModule) Apply(_ context.Context, _ map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	return target.ApplyResult{}, &preflighterr.ModuleError{Module: m.name, Op: "apply", Err: errors.New("only supported on Windows")}
}
