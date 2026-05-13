//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
)

type UserModule struct{}

func (m *UserModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[UserParams](ctx, params, out, pscript.UserCheckScript, nil)
}

func (m *UserModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[UserParams](ctx, params, out, pscript.UserApplyScript, nil)
}
