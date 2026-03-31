package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
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

	facts := &Facts{
		Env: make(map[string]string),
	}

	osFacts, err := g.GatherOS(ctx)
	if err != nil {
		log.Printf("facts: warning: OS fact gathering failed: %v", err)
	} else {
		facts.OS = osFacts
		facts.Hostname = osFacts.Hostname
	}

	disks, err := g.GatherDisks(ctx)
	if err != nil {
		log.Printf("facts: warning: disk fact gathering failed: %v", err)
	} else {
		facts.Disks = disks
	}

	env, err := g.gatherEnv(ctx)
	if err != nil {
		log.Printf("facts: warning: environment fact gathering failed: %v", err)
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
	if err != nil {
		return OSFacts{}, fmt.Errorf("facts: GatherOS: %w", err)
	}

	f := OSFacts{
		Version:  info.OSVersion,
		Arch:     info.Arch,
		Hostname: info.Hostname,
	}

	// Derive Name and Build from OSVersion when it looks like a Windows build
	// string (e.g. "10.0.19041").
	f.Name, f.Build = parseOSVersion(info.OSVersion, info.OSBuild)

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
// On Windows targets (detected via TargetInfo.OSVersion prefix) it runs a
// PowerShell command via target.Execute. On a local non-Windows host it
// delegates to the platform-specific gatherLocalDisks helper. On other
// non-Windows targets an empty list is returned.
func (g *Gatherer) GatherDisks(ctx context.Context) ([]DiskFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := g.target.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("facts: GatherDisks: target info: %w", err)
	}

	isWindows := isWindowsTarget(info.OSVersion)

	if isWindows {
		return g.gatherWindowsDisks(ctx)
	}

	// Local non-Windows: use syscall-based helper (unix.go / stub.go).
	if runtime.GOOS != "windows" {
		return gatherLocalDisks()
	}

	// Remote non-Windows target — not currently supported; return empty.
	return []DiskFacts{}, nil
}

// isWindowsTarget returns true when the OS version string looks like Windows.
func isWindowsTarget(osVersion string) bool {
	lower := strings.ToLower(osVersion)
	return strings.HasPrefix(lower, "10.0") ||
		strings.HasPrefix(lower, "windows") ||
		strings.HasPrefix(lower, "6.")
}

// gatherWindowsDisks runs a PowerShell command against the target to obtain
// filesystem drive information.
func (g *Gatherer) gatherWindowsDisks(ctx context.Context) ([]DiskFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runner, ok := g.target.(interface {
		RunPowerShell(context.Context, string) (string, error)
	})
	if !ok {
		return nil, fmt.Errorf("facts: gatherWindowsDisks: target does not support powershell execution")
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
// On Windows it uses PowerShell; on a local non-Windows host it uses os.Environ.
func (g *Gatherer) gatherEnv(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := g.target.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("facts: gatherEnv: target info: %w", err)
	}

	if isWindowsTarget(info.OSVersion) {
		return g.gatherWindowsEnv(ctx)
	}

	return gatherLocalEnv(), nil
}

// gatherWindowsEnv collects environment variables via PowerShell.
func (g *Gatherer) gatherWindowsEnv(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runner, ok := g.target.(interface {
		RunPowerShell(context.Context, string) (string, error)
	})
	if !ok {
		return nil, fmt.Errorf("facts: gatherWindowsEnv: target does not support powershell execution")
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
