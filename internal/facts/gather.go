package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/bluecadet/preflight/internal/target"
)

// Gatherer collects facts from a target.
type Gatherer struct {
	target target.Target
}

// New creates a new Gatherer backed by the given target.
func New(t target.Target) *Gatherer {
	return &Gatherer{target: t}
}

// Gather collects all facts from the target using a best-effort approach.
// Individual collection failures are logged as warnings rather than returned
// as hard errors so that partial fact sets are still usable.
func (g *Gatherer) Gather(ctx context.Context) (*Facts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, infoErr := g.target.Info(ctx)

	facts := &Facts{
		Env: make(map[string]string),
	}

	osFacts, err := g.gatherOS(info, infoErr)
	if err != nil {
		slog.Warn("fact gathering failed", "category", "os", "error", err)
	} else {
		facts.OS = osFacts
		facts.Hostname = osFacts.Hostname
	}

	disks, err := g.gatherDisks(ctx, info, infoErr)
	if err != nil {
		slog.Warn("fact gathering failed", "category", "disk", "error", err)
	} else {
		facts.Disks = disks
	}

	env, err := g.gatherEnv(ctx, info, infoErr)
	if err != nil {
		slog.Warn("fact gathering failed", "category", "env", "error", err)
	} else {
		facts.Env = env
	}

	return facts, nil
}

// GatherOS collects OS facts using target.Info().
func (g *Gatherer) GatherOS(ctx context.Context) (OSFacts, error) {
	if err := ctx.Err(); err != nil {
		return OSFacts{}, err
	}

	info, err := g.target.Info(ctx)
	return g.gatherOS(info, err)
}

func (g *Gatherer) gatherOS(info target.TargetInfo, infoErr error) (OSFacts, error) {
	if infoErr != nil {
		return OSFacts{}, fmt.Errorf("facts: GatherOS: %w", infoErr)
	}

	f := OSFacts{
		Version:        info.OSVersion,
		Arch:           info.Arch,
		Hostname:       info.Hostname,
		Family:         string(info.OSFamily),
		PackageManager: info.PackageManager,
		Init:           info.Init,
	}

	// Windows keeps deriving the friendly name and integer build from the
	// version/build strings; POSIX uses the os-release ID from the probe and
	// has no build (stays Windows-only).
	if info.OSFamily == target.OSFamilyWindows {
		f.Name, f.Build = parseOSVersion(info.OSVersion, info.OSBuild)
	} else {
		f.Name = info.OSName
	}

	return f, nil
}

// parseOSVersion derives a human-readable OS name and integer build number
// from the raw strings returned by TargetInfo.
func parseOSVersion(osVersion, osBuild string) (name string, build int) {
	// Try the dedicated build field first.
	if osBuild != "" {
		if b, err := strconv.Atoi(osBuild); err == nil {
			build = b
		}
	}

	// Attempt to parse a Windows-style version string like "10.0.19041".
	parts := strings.Split(osVersion, ".")
	if len(parts) >= 3 {
		if b, err := strconv.Atoi(parts[2]); err == nil {
			if build == 0 {
				build = b
			}
			// Map well-known Windows build numbers to friendly names.
			name = windowsBuildName(parts[0]+"."+parts[1], b)
			return name, build
		}
	}

	// Fall back to the raw string as the name.
	return osVersion, build
}

// windowsBuildName returns a friendly Windows name for known major.minor + build combos.
func windowsBuildName(majorMinor string, build int) string {
	if majorMinor != "10.0" {
		return "Windows " + majorMinor
	}
	switch {
	case build >= 22000:
		return "Windows 11"
	case build >= 19041:
		return "Windows 10"
	case build >= 17763:
		return "Windows 10" // 1809
	default:
		return "Windows 10"
	}
}

// GatherDisks collects disk space facts.
// Windows targets are queried through the target's PowerShell transport.
// Local non-Windows hosts use the platform-specific syscall helper.
// Other non-Windows targets currently return an empty list.
func (g *Gatherer) GatherDisks(ctx context.Context) ([]DiskFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := g.target.Info(ctx)

	return g.gatherDisks(ctx, info, err)
}

func (g *Gatherer) gatherDisks(ctx context.Context, info target.TargetInfo, infoErr error) ([]DiskFacts, error) {
	if infoErr != nil {
		return nil, fmt.Errorf("facts: GatherDisks: target info: %w", infoErr)
	}

	if info.IsWindows() {
		return g.gatherWindowsDisks(ctx)
	}

	if info.IsLocal() {
		return gatherLocalDisks()
	}

	return []DiskFacts{}, nil
}

// gatherWindowsDisks runs a PowerShell command against the target to obtain
// filesystem drive information.
func (g *Gatherer) gatherWindowsDisks(ctx context.Context) ([]DiskFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runner, ok := g.target.(target.PowerShellRunner)
	if !ok {
		return nil, fmt.Errorf("facts: gatherWindowsDisks: target does not support PowerShell")
	}
	stdout, err := runner.RunPowerShell(ctx, "Get-PSDrive -PSProvider FileSystem | Select Name,Used,Free | ConvertTo-Json")
	if err != nil {
		return nil, fmt.Errorf("facts: gatherWindowsDisks: execute: %w", err)
	}

	if stdout == "" {
		return []DiskFacts{}, nil
	}

	return parseWindowsDrives([]byte(stdout))
}

// gatherEnv collects environment variables from the target.
// Windows targets use PowerShell. Local non-Windows hosts use the local
// process environment. Remote non-Windows targets currently return an empty map.
func (g *Gatherer) gatherEnv(ctx context.Context, info target.TargetInfo, infoErr error) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if infoErr != nil {
		return nil, fmt.Errorf("facts: gatherEnv: target info: %w", infoErr)
	}

	if info.IsWindows() {
		return g.gatherWindowsEnv(ctx)
	}

	if info.IsLocal() {
		return gatherLocalEnv(), nil
	}

	return map[string]string{}, nil
}

// gatherWindowsEnv collects environment variables via PowerShell.
func (g *Gatherer) gatherWindowsEnv(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runner, ok := g.target.(target.PowerShellRunner)
	if !ok {
		return nil, fmt.Errorf("facts: gatherWindowsEnv: target does not support PowerShell")
	}
	stdout, err := runner.RunPowerShell(ctx, `[System.Environment]::GetEnvironmentVariables() | ConvertTo-Json`)
	if err != nil {
		return nil, fmt.Errorf("facts: gatherWindowsEnv: execute: %w", err)
	}

	if stdout == "" {
		return map[string]string{}, nil
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, fmt.Errorf("facts: gatherWindowsEnv: parse: %w", err)
	}

	env := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			env[k] = s
		}
	}
	return env, nil
}
