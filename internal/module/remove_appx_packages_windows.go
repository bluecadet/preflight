//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type RemoveAppxPackagesModule struct{}

func (m *RemoveAppxPackagesModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return m.CheckWithOutput(ctx, params, nil)
}

func (m *RemoveAppxPackagesModule) CheckWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) (bool, error) {
	if onOutput == nil {
		return runPreparedWindowsCheck(ctx, params, pscript.RemoveAppxCheckScript, winutil.NormalizeRemoveAppxParams)
	}
	return runPreparedWindowsCheckWithOutput(ctx, params, pscript.RemoveAppxCheckScriptWithOutput, winutil.NormalizeRemoveAppxParams, onOutput)
}

func (m *RemoveAppxPackagesModule) Apply(ctx context.Context, params map[string]any) error {
	return m.ApplyWithOutput(ctx, params, nil)
}

func (m *RemoveAppxPackagesModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	return runPreparedWindowsApplyWithOutput(ctx, params, pscript.ModuleRemoveAppxApplyScript, winutil.NormalizeRemoveAppxParams, onOutput)
}
