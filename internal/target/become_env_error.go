package target

import (
	"errors"
	"fmt"
)

// BecomeEnvClass enumerates the environment-prerequisite failures raised
// before Check() when a task needs privileges the target does not provide.
// These join the §7 reason taxonomy as run-log reason codes.
type BecomeEnvClass string

const (
	// ClassRequiresRootViolation: a requires_root module was run with an
	// effective user that is not root. The task must run as root or via
	// become to root.
	ClassRequiresRootViolation BecomeEnvClass = "requires-root-violation"
	// ClassSudoMissing: become is enabled on POSIX but the sudo binary is
	// not present on the target. sudo is required only when become is used.
	ClassSudoMissing BecomeEnvClass = "sudo-missing"
	// ClassSudoPasswordRequired: the no-password sudo wrap (sudo -n) failed
	// because sudo requires a password and none was supplied. Deterministic
	// fail-fast so a password prompt never hangs the run.
	ClassSudoPasswordRequired BecomeEnvClass = "sudo-password-required"
	// ClassSudoAuthFailed: sudo rejected the supplied password (bad
	// password, locked account, etc.).
	ClassSudoAuthFailed BecomeEnvClass = "sudo-auth-failed"
)

// BecomeEnvError is the typed error for POSIX privilege-escalation
// environment failures. It carries the target's runtime kind and a class that
// doubles as the run-log reason code. Every transport constructs and renders
// it the same way via ReasonCodeForError.
type BecomeEnvError struct {
	Class       BecomeEnvClass
	Module      string // empty for non-module errors (e.g. sudo-missing)
	RuntimeKind RuntimeKind
}

// NewRequiresRootViolationError constructs a requires-root-violation error
// for a requires_root module run with a non-root effective user. The wording
// names the module and offers both fixes: run as root, or set become.
func NewRequiresRootViolationError(module string, kind RuntimeKind) *BecomeEnvError {
	return &BecomeEnvError{Class: ClassRequiresRootViolation, Module: module, RuntimeKind: kind}
}

// NewSudoMissingError constructs a sudo-missing error for a POSIX target with
// become enabled but no sudo binary.
func NewSudoMissingError(kind RuntimeKind) *BecomeEnvError {
	return &BecomeEnvError{Class: ClassSudoMissing, RuntimeKind: kind}
}

// NewSudoPasswordRequiredError constructs a sudo-password-required error for a
// no-password sudo wrap that failed because sudo wanted a password.
func NewSudoPasswordRequiredError(kind RuntimeKind) *BecomeEnvError {
	return &BecomeEnvError{Class: ClassSudoPasswordRequired, RuntimeKind: kind}
}

// NewSudoAuthFailedError constructs a sudo-auth-failed error for a sudo run
// that rejected the supplied password.
func NewSudoAuthFailedError(kind RuntimeKind) *BecomeEnvError {
	return &BecomeEnvError{Class: ClassSudoAuthFailed, RuntimeKind: kind}
}

// ReasonCode returns the stable run-log reason code for this error.
func (e *BecomeEnvError) ReasonCode() string { return string(e.Class) }

// Error renders a uniform message. The requires-root class names the module
// and offers both fixes; the sudo classes name what failed.
func (e *BecomeEnvError) Error() string {
	switch e.Class {
	case ClassRequiresRootViolation:
		return fmt.Sprintf(
			"module %q requires root on %s: run as root or set become: {enabled: true} to escalate to root",
			e.Module, e.RuntimeKind)
	case ClassSudoMissing:
		return fmt.Sprintf("become: sudo is required on %s but was not found on the target", e.RuntimeKind)
	case ClassSudoPasswordRequired:
		return fmt.Sprintf("become: sudo requires a password on %s; supply become.password (secret:-backed) or configure NOPASSWD", e.RuntimeKind)
	case ClassSudoAuthFailed:
		return fmt.Sprintf("become: sudo authentication failed on %s (bad password or locked account)", e.RuntimeKind)
	default:
		return fmt.Sprintf("become: %s on %s", e.Class, e.RuntimeKind)
	}
}

// reasonCodeFromBecomeEnv is extracted so ReasonCodeForError can be extended
// without a circular import.
func reasonCodeFromBecomeEnv(err error) (string, bool) {
	var be *BecomeEnvError
	if errors.As(err, &be) {
		return be.ReasonCode(), true
	}
	return "", false
}
