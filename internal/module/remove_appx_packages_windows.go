//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type RemoveAppxPackagesModule struct{}

func (m *RemoveAppxPackagesModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runPreparedWindowsCheck(ctx, params, out, pscript.RemoveAppxCheckScript, winutil.NormalizeRemoveAppxParams)
}

func (m *RemoveAppxPackagesModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runPreparedWindowsApply(ctx, params, out, pscript.RemoveAppxApplyScript, winutil.NormalizeRemoveAppxParams)
}
