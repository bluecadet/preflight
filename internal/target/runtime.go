package target

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
)

type RuntimeKind string

const (
	RuntimeKindWindowsPowerShell RuntimeKind = "windows-powershell"
)

var knownRemoteModules = CatalogNames(CapabilityRemote)

var knownRemoteModuleSet = CatalogSet(CapabilityRemote)

// moduleFuncs lets a Module be registered via function literals rather than
// a concrete type. Used heavily by the Windows PowerShell registry, where
// modules are defined as closures over a script backend.
type moduleFuncs struct {
	check func(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error)
	apply func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error)
}

func (m moduleFuncs) Check(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error) {
	return m.check(ctx, params, out)
}

func (m moduleFuncs) Apply(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
	return m.apply(ctx, params, out)
}

// unsupportedModule returns a Module that fails Check and Apply with the
// given error. Used to populate registry slots for modules the runtime does
// not implement, so dispatch reports a clear error at execution time.
func unsupportedModule(err error) Module {
	return moduleFuncs{
		check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
			return CheckResult{}, err
		},
		apply: func(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
			return ApplyResult{}, err
		},
	}
}

// buildRemoteModuleRegistry assembles a complete ModuleRegistry for a remote
// runtime: it copies the supplied supported entries and fills every other
// known remote-capable module name with an unsupportedModule that surfaces
// the runtime-specific error. It also verifies the supported set against the
// catalog matrix: every entry must be a known remote module AND a module the
// catalog marks as supported on this runtime. A mismatch is a programming
// error (registry drift) and panics.
func buildRemoteModuleRegistry(kind RuntimeKind, supported ModuleRegistry, unsupported func(module string) error) ModuleRegistry {
	registry := make(ModuleRegistry, len(knownRemoteModules))
	for module, impl := range supported {
		if _, ok := knownRemoteModuleSet[module]; !ok {
			panic(fmt.Sprintf("%s runtime: unknown module registration %q", kind, module))
		}
		if !CatalogSupportsRuntime(module, kind) {
			panic(fmt.Sprintf("%s runtime: module %q is registered as supported but the catalog does not list it for this runtime", kind, module))
		}
		registry[module] = impl
	}

	for _, module := range knownRemoteModules {
		if _, ok := registry[module]; ok {
			continue
		}
		registry[module] = unsupportedModule(unsupported(module))
	}

	return registry
}

// executeModule is the single dispatch path used by every Target. It
// resolves the named module from the registry, runs Check then Apply,
// and translates the results into a Result with status, message, and captured output.
func executeModule(
	ctx context.Context,
	taskID string,
	module string,
	params map[string]any,
	dryRun bool,
	onOutput OutputFunc,
	registry ModuleRegistry,
	unsupportedErr func(module string) error,
) (Result, error) {
	mod, ok := registry.Lookup(module)
	if !ok {
		err := unsupportedErr(module)
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	var captured []string
	capture := func(line string) {
		captured = append(captured, line)
		if onOutput != nil {
			onOutput(line)
		}
	}

	checkRes, err := mod.Check(ctx, params, capture)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Output: captured, Error: err}, err
	}
	if !checkRes.NeedsChange {
		return Result{TaskID: taskID, Status: StatusOK, Message: defaultMessage(checkRes.Message, "already in desired state"), Output: captured}, nil
	}
	if dryRun {
		return Result{TaskID: taskID, Status: StatusChanged, Message: defaultMessage(checkRes.Message, "would apply change (dry-run)"), Output: captured}, nil
	}

	applyRes, applyErr := mod.Apply(ctx, params, capture)
	result := Result{TaskID: taskID, Status: StatusChanged, Output: append([]string(nil), captured...)}
	if applyErr != nil {
		// Preserve the legacy distinction: failed apply does not get the
		// "change applied" default message — the renderer would otherwise
		// surface a confusing "failed (change applied)" status line. The
		// module's own message (e.g. a single-line tail of script output)
		// is still propagated when provided.
		result.Status = StatusFailed
		result.Message = strings.TrimSpace(applyRes.Message)
		result.Error = applyErr
		return result, applyErr
	}
	result.Message = defaultMessage(applyRes.Message, "change applied")
	return result, nil
}

func defaultMessage(provided, fallback string) string {
	if trimmed := strings.TrimSpace(provided); trimmed != "" {
		return trimmed
	}
	return fallback
}

// applyStreamed adapts the legacy "Apply returns captured stdout as one
// string" convention to the streaming ApplyResult shape. Lines are split
// from output, forwarded through out, and if output is exactly one line it
// becomes the ApplyResult message (preserving the old "use trailing line
// as result message" behaviour). Whitespace-only output is treated as no
// output at all.
func applyStreamed(output string, out OutputFunc) ApplyResult {
	if strings.TrimSpace(output) == "" {
		return ApplyResult{}
	}
	lines := splitOutputLines(output)
	if out != nil {
		for _, line := range lines {
			out(line)
		}
	}
	if len(lines) == 1 {
		return ApplyResult{Message: lines[0]}
	}
	return ApplyResult{}
}

func splitOutputLines(output string) []string {
	trimmed := strings.TrimRight(output, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// mergePlugins returns a copy of runtimeReg with every bound plugin from
// controllerReg overlaid. Plugins bypass the runtime support matrix —
// controller-side execution makes them supported on every runtime — so they
// override any unsupported stub the runtime registry placed in their slot.
func mergePlugins(runtimeReg, controllerReg ModuleRegistry) ModuleRegistry {
	reg := make(ModuleRegistry, len(runtimeReg))
	maps.Copy(reg, runtimeReg)
	for name, mod := range controllerReg {
		if _, isPlugin := mod.(PluggableModule); isPlugin {
			reg[name] = mod
		}
	}
	return reg
}

// drainRegistry closes every module in reg that implements io.Closer, joining
// all errors. Used by target Close() paths to tear down lazily-spawned plugin
// subprocesses so they are run-scoped, never leaked past the run.
func drainRegistry(reg ModuleRegistry) error {
	var err error
	for _, mod := range reg {
		if closer, ok := mod.(interface{ Close() error }); ok {
			err = errors.Join(err, closer.Close())
		}
	}
	return err
}

// replayBatchOutput calls out once per line of stdout, trimming \r so callers
// receive consistent line endings regardless of the remote host's convention.
// It is the batch counterpart to lineStreamWriter for transports where native
// streaming is unavailable.
func replayBatchOutput(stdout string, out OutputFunc) {
	if out == nil {
		return
	}
	for _, line := range splitOutputLines(stdout) {
		out(strings.TrimSuffix(line, "\r"))
	}
}

func fileContentParam(params map[string]any, label, src string) (string, bool, error) {
	if _, ok := params["content_template"]; ok {
		return "", false, fmt.Errorf("%s: content_template must be rendered before module execution", label)
	}
	value, ok := params["content"]
	if !ok {
		return "", false, nil
	}
	if src != "" {
		return "", false, fmt.Errorf("%s: src and content are mutually exclusive", label)
	}
	content, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s: content must be a string, got %T", label, value)
	}
	return content, true, nil
}
