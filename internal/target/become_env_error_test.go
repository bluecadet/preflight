package target

import (
	"errors"
	"testing"
)

// TestBecomeEnvError_ReasonCodes guards the stable run-log reason codes for the
// become/root environment-prerequisite errors. These join the §7 taxonomy and
// must surface through ReasonCodeForError so task_failed events carry them.
func TestBecomeEnvError_ReasonCodes(t *testing.T) {
	cases := []struct {
		err  *BecomeEnvError
		want string
	}{
		{NewRequiresRootViolationError("system_package", RuntimeKindPOSIXShell), "requires-root-violation"},
		{NewSudoMissingError(RuntimeKindPOSIXShell), "sudo-missing"},
		{NewSudoPasswordRequiredError(RuntimeKindPOSIXShell), "sudo-password-required"},
		{NewSudoAuthFailedError(RuntimeKindPOSIXShell), "sudo-auth-failed"},
	}
	for _, tc := range cases {
		if got := tc.err.ReasonCode(); got != tc.want {
			t.Errorf("ReasonCode: got %q, want %q", got, tc.want)
		}
		if got := ReasonCodeForError(tc.err); got != tc.want {
			t.Errorf("ReasonCodeForError: got %q, want %q", got, tc.want)
		}
	}
}

// TestBecomeEnvError_Messages guards that the wording names what was probed and
// offers both fixes for the requires-root case (run as root, or set become).
func TestBecomeEnvError_Messages(t *testing.T) {
	err := NewRequiresRootViolationError("system_package", RuntimeKindPOSIXShell)
	msg := err.Error()
	for _, want := range []string{"system_package", "root", "become"} {
		if !contains(msg, want) {
			t.Errorf("requires-root message %q missing %q", msg, want)
		}
	}

	sudoMissing := NewSudoMissingError(RuntimeKindPOSIXShell)
	if !contains(sudoMissing.Error(), "sudo") {
		t.Errorf("sudo-missing message: %q", sudoMissing.Error())
	}
}

// TestBecomeEnvError_Wrapping guards that the error survives error.Is/As
// wrapping so ReasonCodeForError can extract it from a wrapped chain.
func TestBecomeEnvError_Wrapping(t *testing.T) {
	inner := NewSudoMissingError(RuntimeKindPOSIXShell)
	wrapped := wrapSSHTargetError("become", inner)
	if got := ReasonCodeForError(wrapped); got != "sudo-missing" {
		t.Errorf("wrapped: got %q, want sudo-missing", got)
	}
	var target *BecomeEnvError
	if !errors.As(wrapped, &target) {
		t.Error("errors.As should unwrap to *BecomeEnvError")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
