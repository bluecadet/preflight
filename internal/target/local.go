package target

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
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

	probeMu sync.Mutex
	probe   *Probe
}

// NewLocalTarget creates a LocalTarget backed by the given registry.
func NewLocalTarget(registry ModuleRegistry) *LocalTarget {
	if registry == nil {
		registry = make(ModuleRegistry)
	}
	cloned := make(ModuleRegistry, len(registry))
	for name, mod := range registry {
		if pluggable, ok := mod.(PluggableModule); ok {
			cloned[name] = pluggable.CloneModule()
			continue
		}
		cloned[name] = mod
	}
	return &LocalTarget{registry: cloned}
}

// Transport identifies the local target connection type.
func (t *LocalTarget) Transport() Transport { return TransportLocal }

// Execute looks up the named module and dispatches through the unified
// executeModule executor. Both the in-process registry path (no become) and
// the become-via-subprocess path share one executor, since both produce
// ModuleRegistry values whose entries satisfy the same Module interface.
func (t *LocalTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error) {
	if opts.Enabled() {
		kind := runtimeKindForLocal()
		become, err := effectiveBecome(kind, opts)
		if err != nil {
			return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
		}

		subReg, err := newSubprocessBecomeRegistry(t.registry, kind, become)
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

// Reachable always returns true for the local target.
func (t *LocalTarget) Reachable(_ context.Context) (bool, error) {
	return true, nil
}

// Info returns basic facts about the local machine. On POSIX hosts the
// cached runtime detection probe is used so Info() and the facts gatherer
// share one detection path; on Windows the probe is not applicable and the
// Go-runtime values are used directly.
func (t *LocalTarget) Info(ctx context.Context) (TargetInfo, error) {
	if runtime.GOOS == "windows" {
		return t.windowsInfo(), nil
	}
	p, err := t.ensureProbe(ctx)
	if err != nil {
		return TargetInfo{}, err
	}
	return TargetInfo{
		Hostname:       p.Hostname,
		OSVersion:      p.OSVersion,
		Arch:           p.Arch,
		OSFamily:       normalizeOSFamily(p.Kernel),
		OSName:         p.OSName,
		PackageManager: p.PackageManager,
		Init:           p.Init,
		Transport:      t.Transport(),
	}, nil
}

func (t *LocalTarget) windowsInfo() TargetInfo {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	return TargetInfo{
		Hostname:  hostname,
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
		OSFamily:  normalizeOSFamily(runtime.GOOS),
		Transport: t.Transport(),
	}
}

// ensureProbe runs the POSIX detection probe once per LocalTarget and caches
// the result. On Windows this is never called.
func (t *LocalTarget) ensureProbe(ctx context.Context) (Probe, error) {
	t.probeMu.Lock()
	defer t.probeMu.Unlock()
	if t.probe != nil {
		return *t.probe, nil
	}
	stdout, err := runLocalSh(ctx, posixProbeScript)
	if err != nil {
		return Probe{}, wrapLocalTargetError("probe", err)
	}
	p := parsePOSIXProbe(stdout)
	t.probe = &p
	return p, nil
}

// runLocalSh executes a POSIX shell script on the local machine and returns its
// combined stdout/stderr. Used only by the local POSIX detection probe.
func runLocalSh(ctx context.Context, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "sh", "-c", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w\noutput: %s", err, string(out))
	}
	return string(out), nil
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
