//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
)

type UserModule struct{}

func (m *UserModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[UserParams](ctx, params, pscript.UserCheckScript, nil)
}

func (m *UserModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[UserParams](ctx, params, pscript.UserApplyScript, nil)
}
