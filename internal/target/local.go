package target

import (
	"context"
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
	return &LocalTarget{registry: registry}
}

// IsLocal implements the localMarker interface used by the facts package.
func (t *LocalTarget) IsLocal() bool { return true }

// Execute looks up the named module, runs Check, and conditionally runs Apply.
// If dryRun is true, Apply is never called.
// If the module implements StreamingModule, ApplyWithOutput is used and lines are forwarded to onOutput.
func (t *LocalTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, dryRun bool, onOutput OutputFunc) (Result, error) {
	mod, ok := t.registry[module]
	if !ok {
		return Result{}, fmt.Errorf("target/local: unknown module %q", module)
	}

	needsChange, err := mod.Check(ctx, params)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	if !needsChange {
		return Result{TaskID: taskID, Status: StatusOK, Message: "already in desired state"}, nil
	}

	// Change is needed but we're in dry-run mode — report what would happen.
	if dryRun {
		return Result{TaskID: taskID, Status: StatusChanged, Message: "would apply change (dry-run)"}, nil
	}

	var captured []string
	captureOnOutput := func(line string) {
		captured = append(captured, line)
		if onOutput != nil {
			onOutput(line)
		}
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
		return fmt.Errorf("target/local: stat src %q: %w", src, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("target/local: read src %q: %w", src, err)
	}
	if err := os.WriteFile(dst, data, info.Mode()); err != nil {
		return fmt.Errorf("target/local: write dst %q: %w", dst, err)
	}
	return nil
}

// ReadFile reads and returns the contents of path on the local machine.
func (t *LocalTarget) ReadFile(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("target/local: read %q: %w", path, err)
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
		return TargetInfo{}, fmt.Errorf("target/local: hostname: %w", err)
	}
	return TargetInfo{
		Hostname:  hostname,
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
	}, nil
}

// RunPowerShell executes a PowerShell script on the local machine.
func (t *LocalTarget) RunPowerShell(ctx context.Context, script string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("target/local: powershell is only available on Windows")
	}
	out, err := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("target/local: powershell failed: %w\noutput: %s", err, string(out))
	}
	return string(out), nil
}
