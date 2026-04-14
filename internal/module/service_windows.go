//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
)

type ServiceModule struct{}

func (m *ServiceModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[ServiceParams](ctx, params, pscript.ServiceCheckScript, nil)
}

func (m *ServiceModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[ServiceParams](ctx, params, pscript.ServiceApplyScript, nil)
}
