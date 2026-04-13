package target

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// errEnsureNotHandled is returned by an ensure function to signal that it
// cannot handle the given params and the caller should fall back to the
// standard check+apply path.
var errEnsureNotHandled = errors.New("ensure not handled")

type RuntimeKind string

const (
	RuntimeKindWindowsPowerShell RuntimeKind = "windows-powershell"
	RuntimeKindPOSIXShell        RuntimeKind = "posix-shell"
)

type remoteModule interface {
	Check(ctx context.Context, params map[string]any, onOutput OutputFunc) (needed bool, message string, err error)
	Apply(ctx context.Context, params map[string]any) (string, error)
}

type remoteModuleRegistry map[string]remoteModule

type remoteModuleFuncs struct {
	check           func(ctx context.Context, params map[string]any) (bool, string, error)
	checkWithOutput func(ctx context.Context, params map[string]any, onOutput OutputFunc) (bool, string, error)
	apply           func(ctx context.Context, params map[string]any) (string, error)
	// ensure, when set, runs check and apply in a single round trip.
	// It returns (changed bool, message string, err error). When dryRun is true
	// it must not apply but may still check and return changed=true.
	ensure func(ctx context.Context, params map[string]any, dryRun bool, onOutput OutputFunc) (bool, string, error)
}

func (m remoteModuleFuncs) Check(ctx context.Context, params map[string]any, onOutput OutputFunc) (bool, string, error) {
	if m.checkWithOutput != nil {
		return m.checkWithOutput(ctx, params, onOutput)
	}
	return m.check(ctx, params)
}

func (m remoteModuleFuncs) Apply(ctx context.Context, params map[string]any) (string, error) {
	return m.apply(ctx, params)
}

func unsupportedRemoteModule(err error) remoteModule {
	return remoteModuleFuncs{
		check: func(context.Context, map[string]any) (bool, string, error) {
			return false, "", err
		},
		apply: func(context.Context, map[string]any) (string, error) {
			return "", err
		},
	}
}

func executeRemoteModule(
	ctx context.Context,
	taskID string,
	module string,
	params map[string]any,
	dryRun bool,
	onOutput OutputFunc,
	registry remoteModuleRegistry,
	unsupportedErr func(module string) error,
) (Result, error) {
	mod, ok := registry[module]
	if !ok {
		err := unsupportedErr(module)
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	var captured []string
	captureOnOutput := func(line string) {
		captured = append(captured, line)
		if onOutput != nil {
			onOutput(line)
		}
	}

	// If the module provides an ensure function, use it to combine check+apply
	// into a single round trip. This is valuable for high-latency transports
	// (e.g. WinRM) where two separate invocations double the overhead.
	// An ensure function may return errEnsureNotHandled to fall back to the
	// standard check+apply path (e.g. when params don't support the fast path).
	if mf, ok := mod.(remoteModuleFuncs); ok && mf.ensure != nil {
		changed, msg, err := mf.ensure(ctx, params, dryRun, captureOnOutput)
		if errors.Is(err, errEnsureNotHandled) {
			goto checkApply
		}
		if err != nil {
			return Result{TaskID: taskID, Status: StatusFailed, Output: captured, Error: err}, err
		}
		if !changed {
			message := "already in desired state"
			if strings.TrimSpace(msg) != "" {
				message = strings.TrimSpace(msg)
			}
			return Result{TaskID: taskID, Status: StatusOK, Message: message, Output: captured}, nil
		}
		message := "change applied"
		if dryRun {
			message = "would apply change (dry-run)"
		}
		if strings.TrimSpace(msg) != "" {
			message = strings.TrimSpace(msg)
		}
		status := StatusChanged
		return Result{TaskID: taskID, Status: status, Message: message, Output: captured}, nil
	}

checkApply:
	needsChange, checkMessage, err := mod.Check(ctx, params, captureOnOutput)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Output: captured, Error: err}, err
	}
	if !needsChange {
		message := "already in desired state"
		if strings.TrimSpace(checkMessage) != "" {
			message = strings.TrimSpace(checkMessage)
		}
		return Result{TaskID: taskID, Status: StatusOK, Message: message, Output: captured}, nil
	}
	if dryRun {
		return Result{TaskID: taskID, Status: StatusChanged, Message: "would apply change (dry-run)", Output: captured}, nil
	}

	applyOutput, err := mod.Apply(ctx, params)
	result := Result{TaskID: taskID, Status: StatusChanged, Message: "change applied", Output: append([]string(nil), captured...)}
	if trimmed := strings.TrimSpace(applyOutput); trimmed != "" {
		applyLines := splitOutputLines(applyOutput)
		result.Output = append(result.Output, applyLines...)
		if len(applyLines) == 1 {
			result.Message = applyLines[0]
		}
		if onOutput != nil {
			for _, line := range applyLines {
				onOutput(line)
			}
		}
	}
	if err != nil {
		result.Status = StatusFailed
		result.Error = err
		return result, err
	}
	return result, nil
}

func splitOutputLines(output string) []string {
	trimmed := strings.TrimRight(output, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func unsupportedRuntimeModuleError(kind RuntimeKind, module string) error {
	return fmt.Errorf("%s runtime: module %q is not supported", kind, module)
}
