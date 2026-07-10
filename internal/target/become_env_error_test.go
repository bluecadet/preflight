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
// wrapping so ReasonCodeForError can extract it from a wrapped chain (the
// run-log reason field is populated from ReasonCodeForError on the execErr).
func TestBecomeEnvError_Wrapping(t *testing.T) {
	all := []struct {
		err  *BecomeEnvError
		want string
	}{
		{NewSudoMissingError(RuntimeKindPOSIXShell), "sudo-missing"},
		{NewRequiresRootViolationError("service", RuntimeKindPOSIXShell), "requires-root-violation"},
		{NewSudoPasswordRequiredError(RuntimeKindPOSIXShell), "sudo-password-required"},
		{NewSudoAuthFailedError(RuntimeKindPOSIXShell), "sudo-auth-failed"},
	}
	for _, tc := range all {
		wrapped := wrapSSHTargetError("become", tc.err)
		if got := ReasonCodeForError(wrapped); got != tc.want {
			t.Errorf("wrapped %s: got %q, want %q", tc.want, got, tc.want)
		}
		var be *BecomeEnvError
		if !errors.As(wrapped, &be) {
			t.Errorf("errors.As should unwrap to *BecomeEnvError for %s", tc.want)
		}
	}
}

// TestEnforcePrivilege_Decision is the spec for the shared pre-Check()
// privilege probe decision logic. It exercises the catalog-decoupled core so
// the requires-root-violation path is covered even before a POSIX-supported
// requires_root module exists in the catalog (the POSIX service/user/etc.
// modules land in a sibling task). supportedOnRuntime is the caller-computed
// catalog matrix result.
func TestEnforcePrivilege_Decision(t *testing.T) {
	rootSession := Probe{EffectiveUID: "0", SudoAvailable: true}
	unprivSession := Probe{EffectiveUID: "1000", SudoAvailable: true}
	noSudo := Probe{EffectiveUID: "1000", SudoAvailable: false}
	rootNoSudo := Probe{EffectiveUID: "0", SudoAvailable: false}

	// bare become to root (user defaulted to root by effectiveBecome)
	becomeRoot := &BecomeOptions{Enabled: true, User: "root", Method: "sudo"}
	becomeAlice := &BecomeOptions{Enabled: true, User: "alice", Method: "sudo"}

	// "svcroot" stands in for a POSIX-supported requires_root module (the real
	// service/user/system_package/reboot modules land in a sibling task).
	// enforcePrivilege reads CatalogRequiresRoot(module), so use a real
	// requires_root catalog name. "service" is requires_root; we pass
	// supportedOnRuntime=true to simulate the sibling's POSIX support.
	rootModule := "service"

	cases := []struct {
		name               string
		module             string
		become             *BecomeOptions
		probe              Probe
		supportedOnRuntime bool
		wantOK             bool
		want               string // reason code, empty when wantOK
	}{
		// requires_root module, no become, session is root -> ok
		{"root session, no become", rootModule, nil, rootSession, true, true, ""},
		// requires_root module, no become, unprivileged -> violation
		{"unpriv session, no become", rootModule, nil, unprivSession, true, false, "requires-root-violation"},
		// requires_root module, become to root -> ok (sudo present)
		{"become root, sudo present", rootModule, becomeRoot, unprivSession, true, true, ""},
		// requires_root module, become to non-root -> violation (become-to-non-root)
		{"become non-root", rootModule, becomeAlice, unprivSession, true, false, "requires-root-violation"},
		// requires_root module, become enabled, no sudo binary -> sudo-missing
		{"become, no sudo binary", rootModule, becomeRoot, noSudo, true, false, "sudo-missing"},
		// non-root-requiring module ignores the root check entirely
		{"non-root module, unpriv session", "shell", nil, unprivSession, true, true, ""},
		{"non-root module, become to non-root", "shell", becomeAlice, unprivSession, true, true, ""},
		// become enabled with no sudo on a non-root module: sudo-missing still
		// fires (become always needs sudo on POSIX).
		{"non-root module, become, no sudo", "shell", becomeRoot, noSudo, true, false, "sudo-missing"},
		// root session, no sudo, no become, root module: ok (sudo not needed)
		{"root session, no sudo, no become", rootModule, nil, rootNoSudo, true, true, ""},
		// requires_root module that is NOT supported on this runtime: no
		// violation (unsupported_on_runtime wins downstream). sudo-missing for
		// become still fires.
		{"unsupported root module, unpriv session", rootModule, nil, unprivSession, false, true, ""},
		{"unsupported root module, become, no sudo", rootModule, becomeRoot, noSudo, false, false, "sudo-missing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := enforcePrivilege(RuntimeKindPOSIXShell, tc.module, tc.become, tc.probe, tc.supportedOnRuntime)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
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
				t.Errorf("reason: got %q, want %q (msg: %s)", be.ReasonCode(), tc.want, be.Error())
			}
		})
	}
}

// TestEnforcePOSIXPrivilege_NonPOSIXIsNoop guards that the enforcement is a
// POSIX-only check: a windows-powershell runtime never fails the privilege
// probe (Windows privilege is the transport account's concern, not become's).
func TestEnforcePOSIXPrivilege_NonPOSIXIsNoop(t *testing.T) {
	if err := enforcePOSIXPrivilege(RuntimeKindWindowsPowerShell, "service", nil, Probe{EffectiveUID: "1000"}); err != nil {
		t.Fatalf("expected nil for windows-powershell, got %v", err)
	}
}

// TestEnforcePOSIXPrivilege_UnsupportedModuleNoViolation guards that a
// requires_root module which is not supported on POSIX (e.g. "service" until
// the POSIX impl lands) does NOT fire requires-root-violation — the
// unsupported_on_runtime error wins downstream.
func TestEnforcePOSIXPrivilege_UnsupportedModuleNoViolation(t *testing.T) {
	err := enforcePOSIXPrivilege(RuntimeKindPOSIXShell, "service", nil, Probe{EffectiveUID: "1000", SudoAvailable: true})
	if err != nil {
		t.Fatalf("expected nil for unsupported-on-POSIX requires_root module, got %v", err)
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
