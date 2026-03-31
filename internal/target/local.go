package target

import (
	"context"
	"fmt"
	"os"
	"runtime"
)

// Module is the interface every built-in (and plugin-backed) module must satisfy.
// Check reports whether a change is needed; Apply makes it so.
// The runner always calls Check first and only calls Apply when Check returns true.
type Module interface {
	// Check returns true when the system is NOT yet in the desired state.
	Check(ctx context.Context, params map[string]interface{}) (needsChange bool, err error)

	// Apply transitions the system to the desired state.
	Apply(ctx context.Context, params map[string]interface{}) error
}

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

// Execute looks up the named module, runs Check, and conditionally runs Apply.
// If dryRun is true, Apply is never called.
func (t *LocalTarget) Execute(ctx context.Context, taskID string, module string, params map[string]interface{}, dryRun bool) (Result, error) {
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

	if err := mod.Apply(ctx, params); err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	return Result{TaskID: taskID, Status: StatusChanged, Message: "change applied"}, nil
}

// CopyFile copies src (local path) to dst (local path).
func (t *LocalTarget) CopyFile(_ context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("target/local: read src %q: %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
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
