//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
)

type PowerPlanModule struct{}

func (m *PowerPlanModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[PowerPlanParams](ctx, params, out, pscript.PowerPlanCheckScript, nil)
}

func (m *PowerPlanModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[PowerPlanParams](ctx, params, out, pscript.PowerPlanApplyScript, nil)
}
