package target

import (
	"errors"

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
