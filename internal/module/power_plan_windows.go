//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
)

type PowerPlanModule struct{}

func (m *PowerPlanModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[PowerPlanParams](ctx, params, pscript.PowerPlanModuleCheckScript, nil)
}

func (m *PowerPlanModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[PowerPlanParams](ctx, params, pscript.PowerPlanModuleApplyScript, nil)
}
