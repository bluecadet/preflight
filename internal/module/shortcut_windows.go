//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
)

type ShortcutModule struct{}

func (m *ShortcutModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[ShortcutParams](ctx, params, pscript.ShortcutCheckScript, nil)
}

func (m *ShortcutModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[ShortcutParams](ctx, params, pscript.ShortcutApplyScript, nil)
}
