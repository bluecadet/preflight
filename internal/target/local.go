package target

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// ModuleRegistry maps module names to their implementations.
type ModuleRegistry map[string]Module

// Lookup returns the Module registered under name, and whether it was found.
func (r ModuleRegistry) Lookup(name string) (Module, bool) {
	m, ok := r[name]
	return m, ok
}

// LocalTarget executes modules in-process on the local machine.
type LocalTarget struct {
	registry ModuleRegistry
}

// NewLocalTarget creates a LocalTarget backed by the given registry.
func NewLocalTarget(registry ModuleRegistry) *LocalTarget {
	if registry == nil {
		registry = make(ModuleRegistry)
	}
	t := &LocalTarget{registry: registry}
	// Each target binds plugins to its own (local) ops backend so the plugin's
	// target effects flow through the handle even locally. *LocalTarget itself
	// implements TargetOps (Exec/PutFile/GetFile/Info).
	bound := make(ModuleRegistry, len(registry))
	for name, mod := range registry {
		if pluggable, ok := mod.(PluggableModule); ok {
			bound[name] = pluggable.BindTarget(t)
			continue
		}
		bound[name] = mod
	}
	t.registry = bound
	return t
}

// Transport identifies the local target connection type.
func (t *LocalTarget) Transport() Transport { return TransportLocal }

// Execute looks up the named module and dispatches through the unified
// executeModule executor. The local target is Windows-only.
func (t *LocalTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error) {
	if runtime.GOOS != "windows" {
		err := errLocalRequiresWindows()
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	become, err := effectiveBecome(RuntimeKindWindowsPowerShell, opts)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	if become != nil {
		subReg, err := newSubprocessBecomeRegistry(t.registry, RuntimeKindWindowsPowerShell, become)
		if err != nil {
			return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
		}

		unsupported := func(module string) error {
			if _, ok := t.registry[module]; ok {
				return wrapLocalTargetError("", fmt.Errorf("module %q does not support become", module))
			}
			return wrapLocalTargetError("", NewUnknownModuleError(module))
		}

		return executeModule(ctx, taskID, module, params, dryRun, onOutput, subReg, unsupported)
	}

	return executeModule(ctx, taskID, module, params, dryRun, onOutput, t.registry, func(module string) error {
		return wrapLocalTargetError("", NewUnknownModuleError(module))
	})
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

// GetFile implements TargetOps: reads the contents of path on the local
// machine. The plugin's target effects flow through the handle even locally.
func (t *LocalTarget) GetFile(ctx context.Context, path string) ([]byte, error) {
	return t.ReadFile(ctx, path)
}

// PutFile implements TargetOps: writes data to path on the local machine.
func (t *LocalTarget) PutFile(_ context.Context, path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return wrapLocalTargetError(fmt.Sprintf("write %q", path), err)
	}
	return nil
}

// Exec implements TargetOps: runs script in PowerShell and returns separated
// stdout/stderr and the exit code.
func (t *LocalTarget) Exec(ctx context.Context, script string) (ExecResult, error) {
	return t.execPowerShell(ctx, script)
}

func (t *LocalTarget) execPowerShell(ctx context.Context, script string) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, wrapLocalTargetError("powershell failed", err)
	}
	return res, nil
}

// Reachable always returns true for the local target.
func (t *LocalTarget) Reachable(_ context.Context) (bool, error) {
	return true, nil
}

// Info returns basic facts about the local machine.
func (t *LocalTarget) Info(_ context.Context) (TargetInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	return TargetInfo{
		Hostname:    hostname,
		OSVersion:   runtime.GOOS,
		Arch:        runtime.GOARCH,
		OSFamily:    normalizeOSFamily(runtime.GOOS),
		RuntimeKind: RuntimeKindWindowsPowerShell,
		Transport:   t.Transport(),
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
// Idempotent: subsequent calls are no-ops once the registry has been drained.
func (t *LocalTarget) Close() error {
	registry := t.registry
	t.registry = nil
	var err error
	for _, mod := range registry {
		closer, ok := mod.(interface{ Close() error })
		if !ok {
			continue
		}
		err = errors.Join(err, closer.Close())
	}
	return err
}
