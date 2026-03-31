package target

import (
	"context"
	"fmt"
)

// WinRMTarget communicates with a remote Windows machine via WinRM.
// All methods are stubs pending full implementation.
type WinRMTarget struct {
	Host     string
	Port     int
	Username string
	Password string
	HTTPS    bool
}

func (t *WinRMTarget) Execute(_ context.Context, _ string, _ string, _ map[string]interface{}, _ bool) (Result, error) {
	return Result{}, fmt.Errorf("winrm: not yet implemented")
}

func (t *WinRMTarget) CopyFile(_ context.Context, _, _ string) error {
	return fmt.Errorf("winrm: not yet implemented")
}

func (t *WinRMTarget) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("winrm: not yet implemented")
}

func (t *WinRMTarget) Reachable(_ context.Context) (bool, error) {
	return false, fmt.Errorf("winrm: not yet implemented")
}

func (t *WinRMTarget) Info(_ context.Context) (TargetInfo, error) {
	return TargetInfo{}, fmt.Errorf("winrm: not yet implemented")
}
