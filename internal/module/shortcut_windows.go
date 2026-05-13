//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
)

type ShortcutModule struct{}

func (m *ShortcutModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[ShortcutParams](ctx, params, out, pscript.ShortcutCheckScript, nil)
}

func (m *ShortcutModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[ShortcutParams](ctx, params, out, pscript.ShortcutApplyScript, nil)
}
