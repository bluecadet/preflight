//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type WingetPackageModule struct{}

func (m *WingetPackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runPreparedWindowsCheck(ctx, params, pscript.WingetPackageCheckScript, winutil.NormalizeWingetParams)
}

func (m *WingetPackageModule) CheckWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) (bool, error) {
	return runPreparedWindowsCheckWithOutput(ctx, params, pscript.WingetPackageCheckScript, winutil.NormalizeWingetParams, onOutput)
}

func (m *WingetPackageModule) Apply(ctx context.Context, params map[string]any) error {
	if err := runPreparedWindowsApply(ctx, params, pscript.WingetPackageApplyScript, winutil.NormalizeWingetParams); err != nil {
		return err
	}
	winutil.RefreshProcessPath()
	return nil
}

func (m *WingetPackageModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	if err := runPreparedWindowsApplyWithOutput(ctx, params, pscript.WingetPackageApplyScript, winutil.NormalizeWingetParams, onOutput); err != nil {
		return err
	}
	winutil.RefreshProcessPath()
	return nil
}
