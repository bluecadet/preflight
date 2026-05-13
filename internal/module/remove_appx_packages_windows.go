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
	script := pscript.RemoveAppxCheckScript
	if out != nil {
		script = pscript.RemoveAppxCheckScriptWithOutput
	}
	return runPreparedWindowsCheck(ctx, params, out, script, winutil.NormalizeRemoveAppxParams)
}

func (m *RemoveAppxPackagesModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runPreparedWindowsApply(ctx, params, out, pscript.ModuleRemoveAppxApplyScript, winutil.NormalizeRemoveAppxParams)
}
