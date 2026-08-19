package target

import (
	"errors"
	"fmt"

	"github.com/bluecadet/preflight/internal/preflighterr"
)

func wrapTargetError(transport Transport, op string, err error) error {
	if err == nil {
		return nil
	}
	var targetErr *preflighterr.TargetError
	if errors.As(err, &targetErr) {
		return err
	}
	return &preflighterr.TargetError{Transport: string(transport), Op: op, Err: err}
}

func wrapLocalTargetError(op string, err error) error {
	return wrapTargetError(TransportLocal, op, err)
}

func wrapSSHTargetError(op string, err error) error {
	return wrapTargetError(TransportSSH, op, err)
}

func wrapWinRMTargetError(op string, err error) error {
	return wrapTargetError(TransportWinRM, op, err)
}

// wrapUnreachable wraps err as a *TargetError the same way wrapTargetError
// does, additionally marking it with preflighterr.ErrUnreachable via a
// standard multi-%w wrap. Use only at call sites where err is the raw,
// not-yet-wrapped error from a call that itself represents establishing
// (or re-establishing) a connection to the target — never at sites where
// the target answered but a command or script itself failed (e.g. a
// non-zero exit code, or a semantic script error), or ordinary task
// failures would misclassify as target_unreachable.
//
// Because wrapTargetError leaves an err that already contains a
// *TargetError untouched (its "don't rewrap" short-circuit), and because
// errors.Is walks the entire chain rather than stopping at the first
// match, it does not matter how many further layers wrap this error
// afterward, or which TargetError ends up outermost by the time IsUnreachable
// is called — the sentinel is never dropped or shadowed. This is the
// property that made classifying by TargetError.Op alone unsafe: Op is
// whatever the outermost wrap call happened to choose, and a re-wrap at a
// higher layer (e.g. remoteWindowsTargetInfo's own "info" wrap) could
// discard the original, more specific Op. errors.Is has no such layer
// dependency.
func wrapUnreachable(transport Transport, op string, err error) error {
	if err == nil {
		return nil
	}
	return wrapTargetError(transport, op, fmt.Errorf("%w: %w", preflighterr.ErrUnreachable, err))
}

func wrapUnreachableSSHError(op string, err error) error {
	return wrapUnreachable(TransportSSH, op, err)
}

func wrapUnreachableWinRMError(op string, err error) error {
	return wrapUnreachable(TransportWinRM, op, err)
}

// IsUnreachable reports whether err represents a failure to reach the
// target at all, rather than a failure of some operation on an
// already-reachable target. Used to classify apply-start connection
// failures as target_unreachable in the run log instead of a generic
// target/task failure. See wrapUnreachable for why errors.Is is safe here
// where Op-string inspection was not.
func IsUnreachable(err error) bool {
	return errors.Is(err, preflighterr.ErrUnreachable)
}
