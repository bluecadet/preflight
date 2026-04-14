//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/winutil"
)

type RegistryModule struct{}

func (m *RegistryModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[RegistryParams](ctx, params, pscript.RegistryModuleCheckScript, winutil.NormalizeRegistryParams)
}

func (m *RegistryModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[RegistryParams](ctx, params, pscript.RegistryApplyScript, winutil.NormalizeRegistryParams)
}
