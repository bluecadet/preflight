//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/target"
)

// PowershellModule runs PowerShell scripts or files.
// Params:
//   - script: inline PowerShell script string
//   - file: path to a .ps1 file
//   - args: list of additional arguments (used with file)
//   - check_script: inline PowerShell script that returns whether a change is needed
//   - creates: path that indicates the operation is already done
type PowershellModule struct{}

func (m *PowershellModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	needed, err := powershellCheckWithOutput(ctx, params, out)
	return target.CheckResult{NeedsChange: needed}, err
}

func (m *PowershellModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return target.ApplyResult{}, powershellApplyWithOutput(ctx, params, out)
}
