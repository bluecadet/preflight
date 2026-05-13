//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type PackageModule struct{}

func (m *PackageModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runPreparedWindowsCheck(ctx, params, out, pscript.PackageCheckScript, winutil.NormalizePackageParams)
}

func (m *PackageModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	res, err := runPreparedWindowsApply(ctx, params, out, pscript.PackageApplyScript, winutil.NormalizePackageParams)
	if err != nil {
		return res, err
	}
	winutil.RefreshProcessPath()
	return res, nil
}
