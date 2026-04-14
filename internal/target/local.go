package target

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// ModuleRegistry maps module names to their implementations.
type ModuleRegistry map[string]Module

// LocalTarget executes modules in-process on the local machine.
type LocalTarget struct {
	registry ModuleRegistry
}

// NewLocalTarget creates a LocalTarget backed by the given registry.
func NewLocalTarget(registry ModuleRegistry) *LocalTarget {
	if registry == nil {
		registry = make(ModuleRegistry)
	}
	cloned := make(ModuleRegistry, len(registry))
	for name, mod := range registry {
		if plugin, ok := mod.(*pluginModule); ok {
			cloned[name] = plugin.clone()
			continue
		}
		cloned[name] = mod
	}
	return &LocalTarget{registry: cloned}
}

// Transport identifies the local target connection type.
func (t *LocalTarget) Transport() Transport { return TransportLocal }

// Execute looks up the named module, runs Check, and conditionally runs Apply.
// If dryRun is true, Apply is never called.
// If the module implements StreamingModule, ApplyWithOutput is used and lines are forwarded to onOutput.
func (t *LocalTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error) {
	if opts.Enabled() {
		kind := runtimeKindForLocal()
		become, err := effectiveBecome(kind, opts)
		if err != nil {
			return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
		}

		unsupported := func(module string) error {
			if _, ok := t.registry[module]; ok {
				return wrapLocalTargetError("", fmt.Errorf("module %q does not support become", module))
			}
			return wrapLocalTargetError("", fmt.Errorf("unknown module %q", module))
		}

		if kind == RuntimeKindWindowsPowerShell {
			backend := &windowsTaskBackend{
				run:       runLocalWindowsPowerShell,
				copyPlain: t.CopyFile,
				tempDir:   localWindowsTempDir(),
				become:    become,
			}
			return executeRemoteModule(ctx, taskID, module, params, dryRun, onOutput, newWindowsPowerShellRegistry(backend), unsupported)
		}

		backend := newLocalPOSIXBackend(become)
		return executeRemoteModule(ctx, taskID, module, params, dryRun, onOutput, newPOSIXShellRegistry(backend), unsupported)
	}

	mod, ok := t.registry[module]
	if !ok {
		return Result{}, wrapLocalTargetError("", fmt.Errorf("unknown module %q", module))
	}

	var captured []string
	captureOnOutput := func(line string) {
		captured = append(captured, line)
		if onOutput != nil {
			onOutput(line)
		}
	}

	var (
		needsChange bool
		err         error
	)
	if sm, ok := mod.(CheckStreamingModule); ok {
		needsChange, err = sm.CheckWithOutput(ctx, params, captureOnOutput)
	} else {
		needsChange, err = mod.Check(ctx, params)
	}
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Output: captured, Error: err}, err
	}

	if !needsChange {
		return Result{TaskID: taskID, Status: StatusOK, Message: "already in desired state", Output: captured}, nil
	}

	// Change is needed but we're in dry-run mode — report what would happen.
	if dryRun {
		return Result{TaskID: taskID, Status: StatusChanged, Message: "would apply change (dry-run)", Output: captured}, nil
	}

	var applyErr error
	if sm, ok := mod.(StreamingModule); ok {
		applyErr = sm.ApplyWithOutput(ctx, params, captureOnOutput)
	} else {
		applyErr = mod.Apply(ctx, params)
	}
	if applyErr != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Output: captured, Error: applyErr}, applyErr
	}

	return Result{TaskID: taskID, Status: StatusChanged, Message: "change applied", Output: captured}, nil
}

// CopyFile copies src (local path) to dst (local path), preserving file permissions.
func (t *LocalTarget) CopyFile(_ context.Context, src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return wrapLocalTargetError(fmt.Sprintf("stat src %q", src), err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return wrapLocalTargetError(fmt.Sprintf("read src %q", src), err)
	}
	if err := os.WriteFile(dst, data, info.Mode()); err != nil {
		return wrapLocalTargetError(fmt.Sprintf("write dst %q", dst), err)
	}
	return nil
}

// ReadFile reads and returns the contents of path on the local machine.
func (t *LocalTarget) ReadFile(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, wrapLocalTargetError(fmt.Sprintf("read %q", path), err)
	}
	return data, nil
}

// Reachable always returns true for the local target.
func (t *LocalTarget) Reachable(_ context.Context) (bool, error) {
	return true, nil
}

// Info returns basic facts about the local machine.
func (t *LocalTarget) Info(_ context.Context) (TargetInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return TargetInfo{}, wrapLocalTargetError("hostname", err)
	}
	return TargetInfo{
		Hostname:  hostname,
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
		OSFamily:  normalizeOSFamily(runtime.GOOS),
		Transport: t.Transport(),
	}, nil
}

// RunPowerShell executes a PowerShell script on the local machine.
func (t *LocalTarget) RunPowerShell(ctx context.Context, script string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", wrapLocalTargetError("", fmt.Errorf("powershell is only available on Windows"))
	}
	out, err := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	).CombinedOutput()
	if err != nil {
		return "", wrapLocalTargetError("powershell failed", fmt.Errorf("%w\noutput: %s", err, string(out)))
	}
	return string(out), nil
}

// Close releases any module-level resources owned by this target instance.
func (t *LocalTarget) Close() error {
	var err error
	for _, mod := range t.registry {
		closer, ok := mod.(interface{ Close() error })
		if !ok {
			continue
		}
		err = errors.Join(err, closer.Close())
	}
	return err
}
