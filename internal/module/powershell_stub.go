//go:build !windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/target"
)

// PowershellModule runs PowerShell scripts or files.
// On non-Windows platforms, attempts to exec `powershell` (WSL/cross-platform compat).
// Params:
//   - script: inline PowerShell script string
//   - file: path to a .ps1 file
//   - args: list of additional arguments (used with file)
//   - check_script: inline PowerShell script that returns whether a change is needed
//   - creates: path that indicates the operation is already done
type PowershellModule struct{}

func (m *PowershellModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return powershellCheck(ctx, params)
}

func (m *PowershellModule) Apply(ctx context.Context, params map[string]any) error {
	return m.ApplyWithOutput(ctx, params, nil)
}

func (m *PowershellModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	return powershellApplyWithOutput(ctx, params, onOutput)
}
