package module

import (
	"context"
)

// RebootModule triggers or checks for a pending system reboot.
// Params:
//   - condition: "if_needed" (default) or "always"
//   - timeout: seconds before reboot (default 60, Windows only)
type RebootModule struct{}

func (m *RebootModule) Check(_ context.Context, params map[string]any) (bool, error) {
	condition, err := paramString(params, "condition", "if_needed")
	if err != nil {
		return false, err
	}
	if condition == "always" {
		return true, nil
	}
	return rebootPending()
}

func (m *RebootModule) Apply(_ context.Context, params map[string]any) error {
	timeout, err := paramInt(params, "timeout", 60)
	if err != nil {
		return err
	}
	return applyReboot(timeout)
}
