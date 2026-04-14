//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/winutil"
)

type WingetPackageModule struct{}

func (m *WingetPackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runPreparedWindowsCheck(ctx, params, pscript.WingetPackageCheckScript, winutil.NormalizeWingetParams)
}

func (m *WingetPackageModule) Apply(ctx context.Context, params map[string]any) error {
	return runPreparedWindowsApply(ctx, params, pscript.WingetPackageApplyScript, winutil.NormalizeWingetParams)
}
