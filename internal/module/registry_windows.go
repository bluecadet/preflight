//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type RegistryModule struct{}

func (m *RegistryModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[RegistryParams](ctx, params, out, pscript.RegistryCheckScript, winutil.NormalizeRegistryParams)
}

func (m *RegistryModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[RegistryParams](ctx, params, out, pscript.RegistryApplyScript, winutil.NormalizeRegistryParams)
}
