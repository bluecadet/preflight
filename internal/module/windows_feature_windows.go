//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
)

type WindowsFeatureModule struct{}

func (m *WindowsFeatureModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[WindowsFeatureParams](ctx, params, out, pscript.WindowsFeatureCheckScript, nil)
}

func (m *WindowsFeatureModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[WindowsFeatureParams](ctx, params, out, pscript.WindowsFeatureApplyScript, nil)
}
