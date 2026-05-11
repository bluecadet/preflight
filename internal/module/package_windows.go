//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/winutil"
)

type PackageModule struct{}

func (m *PackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runPreparedWindowsCheck(ctx, params, pscript.PackageCheckScript, winutil.NormalizePackageParams)
}

func (m *PackageModule) Apply(ctx context.Context, params map[string]any) error {
	if err := runPreparedWindowsApply(ctx, params, pscript.PackageApplyScript, winutil.NormalizePackageParams); err != nil {
		return err
	}
	winutil.RefreshProcessPath()
	return nil
}
