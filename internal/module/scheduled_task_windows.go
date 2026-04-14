//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/winutil"
)

type ScheduledTaskModule struct{}

func (m *ScheduledTaskModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[ScheduledTaskParams](ctx, params, pscript.ScheduledTaskCheckScript, normalizeScheduledTaskModuleParams)
}

func (m *ScheduledTaskModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[ScheduledTaskParams](ctx, params, pscript.ScheduledTaskApplyScript, normalizeScheduledTaskModuleParams)
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
