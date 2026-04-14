//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
)

type WindowsFeatureModule struct{}

func (m *WindowsFeatureModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[WindowsFeatureParams](ctx, params, pscript.WindowsFeatureModuleCheckScript, nil)
}

func (m *WindowsFeatureModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[WindowsFeatureParams](ctx, params, pscript.WindowsFeatureApplyScript, nil)
}
