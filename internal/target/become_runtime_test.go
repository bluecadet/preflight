package target

import (
	"strings"
	"testing"
)

// TestEffectiveBecome_WindowsBareBecomeRequiresUser guards that Windows still
// rejects a bare become with no user.
func TestEffectiveBecome_WindowsBareBecomeRequiresUser(t *testing.T) {
	opts := ExecutionOptions{Become: &BecomeOptions{Enabled: true}}
	_, err := effectiveBecome(RuntimeKindWindowsPowerShell, opts)
	if err == nil {
		t.Fatal("expected error for bare Windows become, got nil")
	}
	if !strings.Contains(err.Error(), "user is required") {
		t.Errorf("expected user-required error, got: %v", err)
	}
}
