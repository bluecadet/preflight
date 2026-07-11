package target

import (
	"context"
	"fmt"
	"strings"
)

// posixServicePrerequisiteDetail names what was probed and what the service
// module requires, so the missing_prerequisite error is actionable.
const posixServicePrerequisiteDetail = "systemd not detected (probed /run/systemd/system); service module requires systemd"

// posixServiceRequireSystemd returns a typed missing_prerequisite error when
// the cached init-system signal is absent. The spec (§6) has modules read the
// runtime detection probe rather than re-probing, so there is no second
// detection path here.
func posixServiceRequireSystemd(backend posixShellBackend) error {
	if backend.InitSystem() != "systemd" {
		return NewMissingPrerequisiteError("service", RuntimeKindPOSIXShell, posixServicePrerequisiteDetail)
	}
	return nil
}

// parsePOSIXServiceParams validates the service module params. state and
// startup_type are both optional; an empty pair is a no-op (nothing to
// converge), mirroring the Windows module.
func parsePOSIXServiceParams(params map[string]any) (name, state, startupType string, err error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return "", "", "", fmt.Errorf("service: required param %q is missing", "name")
	}
	state, _ = params["state"].(string)
	switch state {
	case "", "running", "stopped", "disabled":
	default:
		return "", "", "", fmt.Errorf("service: unknown state %q (want running|stopped|disabled)", state)
	}
	startupType, _ = params["startup_type"].(string)
	switch startupType {
	case "", "automatic", "manual", "disabled":
	default:
		return "", "", "", fmt.Errorf("service: unknown startup_type %q (want automatic|manual|disabled)", startupType)
	}
	return name, state, startupType, nil
}

// posixServiceStartupTarget maps the schema startup_type to the systemctl
// is-enabled value it should converge to: automatic -> enabled,
// manual -> disabled, disabled -> masked (spec §4).
func posixServiceStartupTarget(startupType string) string {
	switch startupType {
	case "automatic":
		return "enabled"
	case "manual":
		return "disabled"
	case "disabled":
		return "masked"
	}
	return ""
}

// posixServiceQueryState reads the current active and enabled state of a unit
// in a single round trip. systemctl is-active/is-enabled exit non-zero for
// inactive/disabled units, so they run under $(...) command substitution whose
// exit code is discarded; the overall script exits with printf's code (0). A
// unit that does not exist surfaces as enabled=not-found.
func posixServiceQueryState(ctx context.Context, backend posixShellBackend, name string) (active, enabled string, err error) {
	command := fmt.Sprintf(
		"a=$(systemctl is-active %q 2>/dev/null); e=$(systemctl is-enabled %q 2>/dev/null); printf 'active=%%s\\nenabled=%%s\\n' \"${a:-unknown}\" \"${e:-unknown}\"",
		name, name)
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, command, nil)
	if err != nil {
		return "", "", err
	}
	if code != 0 {
		return "", "", fmt.Errorf("service: systemctl query exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "active":
			active = v
		case "enabled":
			enabled = v
		}
	}
	return active, enabled, nil
}

func checkPOSIXService(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	name, state, startupType, err := parsePOSIXServiceParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	if err := posixServiceRequireSystemd(backend); err != nil {
		return CheckResult{}, err
	}
	// Nothing to converge: no state and no startup_type requested.
	if state == "" && startupType == "" {
		return CheckResult{}, nil
	}
	active, enabled, err := posixServiceQueryState(ctx, backend, name)
	if err != nil {
		return CheckResult{}, err
	}
	if enabled == "not-found" {
		return CheckResult{}, fmt.Errorf("service: %q is not a loaded systemd unit", name)
	}
	needs := false
	if state == "disabled" {
		// state=disabled wants the unit stopped and masked (Windows disabled ~=
		// systemd masked, spec §4). It short-circuits startup_type like Windows.
		if active != "inactive" {
			needs = true
		}
		if enabled != "masked" {
			needs = true
		}
	} else {
		switch state {
		case "running":
			if active != "active" {
				needs = true
			}
		case "stopped":
			if active != "inactive" {
				needs = true
			}
		}
		if startupType != "" {
			if enabled != posixServiceStartupTarget(startupType) {
				needs = true
			}
		}
	}
	return CheckResult{NeedsChange: needs}, nil
}

func applyPOSIXService(ctx context.Context, backend posixShellBackend, params map[string]any) error {
	name, state, startupType, err := parsePOSIXServiceParams(params)
	if err != nil {
		return err
	}
	if err := posixServiceRequireSystemd(backend); err != nil {
		return err
	}
	if state == "" && startupType == "" {
		return nil
	}
	if state == "disabled" {
		// Stop (best-effort: a masked/stopped unit is fine) then mask. Short-circuits
		// startup_type, mirroring the Windows apply.
		if err := posixMustRun(ctx, backend, fmt.Sprintf("systemctl stop %q; systemctl mask %q", name, name)); err != nil {
			return err
		}
		return nil
	}
	// Apply startup_type first so a masked unit can transition to enabled/
	// disabled: unmask is a no-op when the unit is not masked.
	if startupType != "" {
		switch startupType {
		case "automatic":
			if err := posixMustRun(ctx, backend, fmt.Sprintf("systemctl unmask %q 2>/dev/null; systemctl enable %q", name, name)); err != nil {
				return err
			}
		case "manual":
			if err := posixMustRun(ctx, backend, fmt.Sprintf("systemctl unmask %q 2>/dev/null; systemctl disable %q", name, name)); err != nil {
				return err
			}
		case "disabled":
			if err := posixMustRun(ctx, backend, fmt.Sprintf("systemctl mask %q", name)); err != nil {
				return err
			}
		}
	}
	switch state {
	case "running":
		if err := posixMustRun(ctx, backend, fmt.Sprintf("systemctl start %q", name)); err != nil {
			return err
		}
	case "stopped":
		if err := posixMustRun(ctx, backend, fmt.Sprintf("systemctl stop %q", name)); err != nil {
			return err
		}
	}
	return nil
}