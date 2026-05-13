//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type ScheduledTaskModule struct{}

func (m *ScheduledTaskModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[ScheduledTaskParams](ctx, params, out, pscript.ScheduledTaskCheckScript, normalizeScheduledTaskModuleParams)
}

func (m *ScheduledTaskModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[ScheduledTaskParams](ctx, params, out, pscript.ScheduledTaskApplyScript, normalizeScheduledTaskModuleParams)
}

func normalizeScheduledTaskModuleParams(params map[string]any) (map[string]any, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return nil, err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}
