//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
)

type ServiceModule struct{}

func (m *ServiceModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[ServiceParams](ctx, params, out, pscript.ServiceCheckScript, nil)
}

func (m *ServiceModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[ServiceParams](ctx, params, out, pscript.ServiceApplyScript, nil)
}
