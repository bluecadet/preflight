package target

import (
	"errors"
	"strings"
	"testing"
)

// TestEffectiveBecome_POSIXBareBecomeDefaultsToRoot guards the §5 fix: bare
// `become: {enabled: true}` (no user) means root on POSIX. Windows keeps
// requiring an explicit user.
func TestEffectiveBecome_POSIXBareBecomeDefaultsToRoot(t *testing.T) {
	opts := ExecutionOptions{Become: &BecomeOptions{Enabled: true}}
	become, err := effectiveBecome(RuntimeKindPOSIXShell, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if become.User != "root" {
		t.Errorf("bare POSIX become user: got %q, want root", become.User)
	}
	if become.Method != "sudo" {
		t.Errorf("POSIX become method: got %q, want sudo", become.Method)
	}
}

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

// TestEffectiveBecome_ExplicitPOSIXUserPreserved guards a named POSIX user is
// preserved (not overwritten with root).
func TestEffectiveBecome_ExplicitPOSIXUserPreserved(t *testing.T) {
	opts := ExecutionOptions{Become: &BecomeOptions{Enabled: true, User: "alice"}}
	become, err := effectiveBecome(RuntimeKindPOSIXShell, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if become.User != "alice" {
		t.Errorf("explicit POSIX user: got %q, want alice", become.User)
	}
}

// TestWrapPOSIXBecome_NoPasswordUsesSudoN guards the §5 fail-fast: the
// no-password wrap uses `sudo -n` so a password-requiring sudo fails
// deterministically instead of hanging.
func TestWrapPOSIXBecome_NoPasswordUsesSudoN(t *testing.T) {
	become := &BecomeOptions{User: "root", Method: "sudo"}
	wrapped, _ := wrapPOSIXBecome("echo hi", nil, become)
	if !strings.Contains(wrapped, "sudo -n") {
		t.Errorf("no-password wrap missing `sudo -n`: %q", wrapped)
	}
	if !strings.Contains(wrapped, "-u 'root'") {
		t.Errorf("wrap missing `-u 'root'`: %q", wrapped)
	}
}

// TestWrapPOSIXBecome_PasswordUsesSudoS guards that a supplied password uses
// `sudo -S` (stdin password) and does NOT add -n.
func TestWrapPOSIXBecome_PasswordUsesSudoS(t *testing.T) {
	become := &BecomeOptions{User: "root", Method: "sudo", Password: "secret"}
	wrapped, stdin := wrapPOSIXBecome("echo hi", nil, become)
	if !strings.Contains(wrapped, "sudo -S") {
		t.Errorf("password wrap missing `sudo -S`: %q", wrapped)
	}
	if strings.Contains(wrapped, "sudo -n") {
		t.Errorf("password wrap must not use `sudo -n`: %q", wrapped)
	}
	if !strings.HasPrefix(string(stdin), "secret\n") {
		t.Errorf("stdin missing password prefix: %q", string(stdin))
	}
}

// TestWrapPOSIXBecome_NilIsPassthrough guards no become means no wrapping.
func TestWrapPOSIXBecome_NilIsPassthrough(t *testing.T) {
	wrapped, stdin := wrapPOSIXBecome("echo hi", []byte("x"), nil)
	if wrapped != "echo hi" {
		t.Errorf("nil become should not wrap: got %q", wrapped)
	}
	if string(stdin) != "x" {
		t.Errorf("nil become should pass stdin through: got %q", string(stdin))
	}
}

// TestClassifySudoFailure guards the sudo failure -> typed reason code mapping
// so the run log carries sudo-password-required / sudo-auth-failed.
func TestClassifySudoFailure(t *testing.T) {
	noPass := &BecomeOptions{User: "root", Method: "sudo"}
	withPass := &BecomeOptions{User: "root", Method: "sudo", Password: "secret"}

	cases := []struct {
		name   string
		become *BecomeOptions
		stderr string
		want   string // reason code, empty for nil
	}{
		{"no password, sudo -n wants password", noPass, "sudo: a password is required\n", "sudo-password-required"},
		{"bad password, sorry try again", withPass, "Sorry, try again.\n", "sudo-auth-failed"},
		{"bad password, authentication failure", withPass, "sudo: authentication failure\n", "sudo-auth-failed"},
		{"unrelated non-zero exit", noPass, "command not found\n", ""},
		{"nil become", nil, "sudo: a password is required\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifySudoFailure(tc.become, tc.stderr)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var be *BecomeEnvError
			if !errors.As(err, &be) {
				t.Fatalf("expected *BecomeEnvError, got %T: %v", err, err)
			}
			if be.ReasonCode() != tc.want {
				t.Errorf("reason: got %q, want %q", be.ReasonCode(), tc.want)
			}
		})
	}
}
