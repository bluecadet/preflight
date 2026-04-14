//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/target"
)

type windowsParamsPreparer func(map[string]any) (map[string]any, error)

func runValidatedWindowsCheck[T any](ctx context.Context, params map[string]any, script string, prepare windowsParamsPreparer) (bool, error) {
	if err := validateWindowsParams[T](params); err != nil {
		return false, err
	}
	return runPreparedWindowsCheck(ctx, params, script, prepare)
}

func runValidatedWindowsApply[T any](ctx context.Context, params map[string]any, script string, prepare windowsParamsPreparer) error {
	if err := validateWindowsParams[T](params); err != nil {
		return err
	}
	return runPreparedWindowsApply(ctx, params, script, prepare)
}

func runPreparedWindowsCheck(ctx context.Context, params map[string]any, script string, prepare windowsParamsPreparer) (bool, error) {
	prepared, err := prepareWindowsParams(params, prepare)
	if err != nil {
		return false, err
	}
	return runWindowsPowerShellBool(ctx, prepared, script)
}

func runPreparedWindowsApply(ctx context.Context, params map[string]any, script string, prepare windowsParamsPreparer) error {
	prepared, err := prepareWindowsParams(params, prepare)
	if err != nil {
		return err
	}
	_, err = runWindowsPowerShellWithParams(ctx, prepared, script)
	return err
}

func runPreparedWindowsCheckWithOutput(ctx context.Context, params map[string]any, script string, prepare windowsParamsPreparer, onOutput target.OutputFunc) (bool, error) {
	prepared, err := prepareWindowsParams(params, prepare)
	if err != nil {
		return false, err
	}
	if onOutput == nil {
		return runWindowsPowerShellBool(ctx, prepared, script)
	}
	return runWindowsPowerShellBoolWithOutput(ctx, prepared, script, onOutput)
}

func runPreparedWindowsApplyWithOutput(ctx context.Context, params map[string]any, script string, prepare windowsParamsPreparer, onOutput target.OutputFunc) error {
	prepared, err := prepareWindowsParams(params, prepare)
	if err != nil {
		return err
	}
	return runWindowsPowerShellWithParamsWithOutput(ctx, prepared, script, onOutput)
}

func prepareWindowsParams(params map[string]any, prepare windowsParamsPreparer) (map[string]any, error) {
	if prepare == nil {
		return params, nil
	}
	return prepare(params)
}

func validateWindowsParams[T any](params map[string]any) error {
	var decoded T
	return Decode(params, &decoded)
}
