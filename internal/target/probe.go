package target

import "strings"

// Probe holds the lazily-collected POSIX runtime detection signals for a
// target. One probe runs per target per run; it is cached so that Info()
// and the facts gatherer read the same result without re-probing, leaving no
// second detection path.
//
// Absent signals are empty strings: a missing source (e.g. no os-release on
// macOS, no supported package manager) empties that field without failing the
// probe. Only a transport-level failure errors, via the existing Info()
// error path.
//
// This struct is also the future home of the effective-uid /
// sudo-availability ticket (internal runtime state, not exposed as facts) and
// the enriched TargetInfo handed to plugins.
type Probe struct {
	Hostname       string
	Kernel         string // uname -s
	Arch           string // uname -m
	OSName         string // os-release ID (e.g. "ubuntu", "rocky")
	OSVersion      string // os-release VERSION_ID
	PackageManager string // "apt" | "dnf" | ""
	Init           string // "systemd" | ""
}

// posixProbeScript is the single shell script that gathers every POSIX
// detection signal in one round trip. Roster — nothing more:
//   - hostname
//   - uname -s / uname -m
//   - os-release ID and VERSION_ID (sourced from /etc/os-release, falling
//     back to /usr/lib/os-release)
//   - command -v apt-get, else command -v dnf
//   - test -d /run/systemd/system
//
// Each signal is emitted as a key=value line so the parser is order-independent
// and naturally tolerant of absent signals (empty values).
const posixProbeScript = `os_name=""
os_version=""
for f in /etc/os-release /usr/lib/os-release; do
	if [ -f "$f" ]; then
		. "$f"
		os_name="$ID"
		os_version="$VERSION_ID"
		break
	fi
done
package_manager=""
if command -v apt-get >/dev/null 2>&1; then
	package_manager=apt
elif command -v dnf >/dev/null 2>&1; then
	package_manager=dnf
fi
init=""
if [ -d /run/systemd/system ]; then
	init=systemd
fi
printf 'hostname=%s\n' "$(hostname)"
printf 'kernel=%s\n' "$(uname -s)"
printf 'arch=%s\n' "$(uname -m)"
printf 'os_name=%s\n' "$os_name"
printf 'os_version=%s\n' "$os_version"
printf 'package_manager=%s\n' "$package_manager"
printf 'init=%s\n' "$init"
`

// parsePOSIXProbe parses the output of posixProbeScript into a Probe. It is
// defensive per-signal: known keys populate their fields, unknown keys are
// ignored, and missing or truncated lines leave the corresponding field empty.
// It never returns an error — only a transport-level failure (handled by the
// Info() callers) aborts detection.
func parsePOSIXProbe(stdout string) Probe {
	var p Probe
	for line := range strings.SplitSeq(stdout, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "hostname":
			p.Hostname = val
		case "kernel":
			p.Kernel = val
		case "arch":
			p.Arch = val
		case "os_name":
			p.OSName = val
		case "os_version":
			p.OSVersion = val
		case "package_manager":
			p.PackageManager = val
		case "init":
			p.Init = val
		}
	}
	return p
}
