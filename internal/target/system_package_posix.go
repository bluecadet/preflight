package target

import (
	"context"
	"fmt"
	"strings"
)

// system_package_spec is one resolved entry from the system_package packages
// list. version is empty when no pin is set; ensure is always present|absent.
type systemPackageSpec struct {
	name    string
	version string
	ensure  string
}

// parseSystemPackageParams resolves the packages list from params into
// validated specs. It mirrors the winget_package list shape but uses name
// (not id) and an optional version pin. Defaults ensure to present.
func parseSystemPackageParams(params map[string]any) ([]systemPackageSpec, error) {
	raw, ok := params["packages"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("system_package: required param %q is missing", "packages")
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("system_package: packages must be a list, got %T", raw)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("system_package: packages list must not be empty")
	}
	specs := make([]systemPackageSpec, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("system_package: packages[%d] must be a map, got %T", i, item)
		}
		name, _ := m["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("system_package: packages[%d] required field %q is missing or empty", i, "name")
		}
		version, _ := m["version"].(string)
		ensure, _ := m["ensure"].(string)
		if ensure == "" {
			ensure = "present"
		}
		if ensure != "present" && ensure != "absent" {
			return nil, fmt.Errorf("system_package: packages[%d] ensure must be present|absent, got %q", i, ensure)
		}
		specs = append(specs, systemPackageSpec{name: name, version: version, ensure: ensure})
	}
	return specs, nil
}

// systemPackagePayload renders specs as pipe-delimited lines read by the
// check/apply scripts over stdin: name|version|ensure. Pipe is safe because
// package names and native version strings never contain it.
func systemPackagePayload(specs []systemPackageSpec) []byte {
	var b strings.Builder
	for _, s := range specs {
		fmt.Fprintf(&b, "%s|%s|%s\n", s.name, s.version, s.ensure)
	}
	return []byte(b.String())
}

// posixSystemPackageMissingPrerequisiteDetail is the detail string used when
// the cached probe could not detect apt or dnf. Reused by Check and Apply so
// the wording stays uniform.
const posixSystemPackageMissingPrerequisiteDetail = "requires apt-get or dnf on the remote host"

// resolvePOSIXPackageManager reads the cached package-manager fact from the
// backend's probe and returns the typed missing-prerequisite error when it is
// absent. The fact comes from the cached detection probe (one round trip per
// target), not a fresh re-detection, so there is no per-task detection cost.
func resolvePOSIXPackageManager(ctx context.Context, backend posixShellBackend) (string, error) {
	pm, err := backend.PackageManager(ctx)
	if err != nil {
		return "", err
	}
	if pm == "" {
		return "", NewMissingPrerequisiteError("system_package", RuntimeKindPOSIXShell, posixSystemPackageMissingPrerequisiteDetail)
	}
	if pm != "apt" && pm != "dnf" {
		return "", NewMissingPrerequisiteError("system_package", RuntimeKindPOSIXShell, fmt.Sprintf("unsupported package manager %q: %s", pm, posixSystemPackageMissingPrerequisiteDetail))
	}
	return pm, nil
}

func checkPOSIXSystemPackage(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	pm, err := resolvePOSIXPackageManager(ctx, backend)
	if err != nil {
		return CheckResult{}, err
	}
	specs, err := parseSystemPackageParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	script := posixSystemPackageAptCheckScript
	if pm == "dnf" {
		script = posixSystemPackageDnfCheckScript
	}
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, script, systemPackagePayload(specs))
	if err != nil {
		return CheckResult{}, err
	}
	if code != 0 {
		return CheckResult{}, fmt.Errorf("system_package: check exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stdout) == "change" {
		return CheckResult{NeedsChange: true}, nil
	}
	return CheckResult{}, nil
}

func applyPOSIXSystemPackage(ctx context.Context, backend posixShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	pm, err := resolvePOSIXPackageManager(ctx, backend)
	if err != nil {
		return ApplyResult{}, err
	}
	specs, err := parseSystemPackageParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	script := posixSystemPackageAptApplyScript
	if pm == "dnf" {
		script = posixSystemPackageDnfApplyScript
	}
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, script, systemPackagePayload(specs))
	if err != nil {
		return ApplyResult{}, err
	}
	if code != 0 {
		return ApplyResult{}, fmt.Errorf("system_package: apply exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return applyStreamed(stdout, out), nil
}

// posixSystemPackageAptCheckScript reads name|version|ensure specs from stdin
// and prints "change" when any spec is not in the desired state, else "ok".
// It never exits non-zero: a missing package is a state difference, not an
// error. dpkg -s in a conditional is the installed probe; dpkg-query supplies
// the installed version for pin comparison. The version pin is compared as an
// exact string against dpkg-query's ${Version} output (the native Debian
// version, including epoch/revision) — a documented limitation.
const posixSystemPackageAptCheckScript = `needs=0
while IFS='|' read -r name version ensure; do
	[ -z "$name" ] && continue
	if [ "$ensure" = "absent" ]; then
		if dpkg -s "$name" >/dev/null 2>&1; then needs=1; fi
	else
		if ! dpkg -s "$name" >/dev/null 2>&1; then
			needs=1
		elif [ -n "$version" ]; then
			inst=$(dpkg-query -W -f='${Version}' "$name" 2>/dev/null || true)
			if [ "$inst" != "$version" ]; then needs=1; fi
		fi
	fi
done
if [ "$needs" = "1" ]; then printf 'change\n'; else printf 'ok\n'; fi
`

// posixSystemPackageAptApplyScript reads name|version|ensure specs from stdin
// and installs/removes each, idempotently skipping specs already in the
// desired state. set -e surfaces a failed install/remove as a non-zero exit
// so the caller reports a failed apply. DEBIAN_FRONTEND keeps prompts out of
// non-interactive runs.
const posixSystemPackageAptApplyScript = `set -e
export DEBIAN_FRONTEND=noninteractive
while IFS='|' read -r name version ensure; do
	[ -z "$name" ] && continue
	if [ "$ensure" = "absent" ]; then
		if dpkg -s "$name" >/dev/null 2>&1; then
			apt-get remove -y "$name"
		fi
	else
		if [ -n "$version" ]; then
			inst=$(dpkg-query -W -f='${Version}' "$name" 2>/dev/null || true)
			if [ "$inst" != "$version" ]; then
				apt-get install -y --no-install-recommends "$name=$version"
			fi
		else
			if ! dpkg -s "$name" >/dev/null 2>&1; then
				apt-get install -y --no-install-recommends "$name"
			fi
		fi
	fi
done
`

// posixSystemPackageDnfCheckScript is the dnf/rpm counterpart. rpm -q is the
// installed probe; rpm -q --qf supplies VERSION-RELEASE for pin comparison.
// The version pin is compared as an exact string against VERSION-RELEASE — a
// documented limitation (the user supplies the full native version string).
const posixSystemPackageDnfCheckScript = `needs=0
while IFS='|' read -r name version ensure; do
	[ -z "$name" ] && continue
	if [ "$ensure" = "absent" ]; then
		if rpm -q "$name" >/dev/null 2>&1; then needs=1; fi
	else
		if ! rpm -q "$name" >/dev/null 2>&1; then
			needs=1
		elif [ -n "$version" ]; then
			inst=$(rpm -q --qf '%{VERSION}-%{RELEASE}' "$name" 2>/dev/null || true)
			if [ "$inst" != "$version" ]; then needs=1; fi
		fi
	fi
done
if [ "$needs" = "1" ]; then printf 'change\n'; else printf 'ok\n'; fi
`

// posixSystemPackageDnfApplyScript is the dnf/rpm counterpart. dnf install
// accepts name-version (NEVRA substring) for a pinned install.
const posixSystemPackageDnfApplyScript = `set -e
while IFS='|' read -r name version ensure; do
	[ -z "$name" ] && continue
	if [ "$ensure" = "absent" ]; then
		if rpm -q "$name" >/dev/null 2>&1; then
			dnf remove -y "$name"
		fi
	else
		if [ -n "$version" ]; then
			inst=$(rpm -q --qf '%{VERSION}-%{RELEASE}' "$name" 2>/dev/null || true)
			if [ "$inst" != "$version" ]; then
				dnf install -y "$name-$version"
			fi
		else
			if ! rpm -q "$name" >/dev/null 2>&1; then
				dnf install -y "$name"
			fi
		fi
	fi
done
`
