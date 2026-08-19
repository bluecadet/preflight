//go:build !windows

package module

import (
	"fmt"
	"os/exec"
	"strings"
)

// platformPowerShellCandidates lists local PowerShell binary names to
// resolve, in preference order. pwsh is PowerShell Core -- the binary
// Microsoft ships for macOS and Linux -- checked first; "powershell"
// (Windows PowerShell's binary name) is checked afterward in case it has
// been aliased or installed under that name.
var platformPowerShellCandidates = []string{"pwsh", "powershell"}

// platformLookPath resolves name to an executable path exactly like
// exec.LookPath. It is a package-level var so tests can fake PATH lookups
// without needing real binaries installed.
var platformLookPath = exec.LookPath

// platformPowerShellBinary resolves the local PowerShell executable to run,
// searching platformPowerShellCandidates in order. If none are found, it
// returns a clear, actionable error naming every binary searched -- a bare
// exec error ("executable file not found in $PATH") would not tell the user
// that installing PowerShell (pwsh) is what's missing.
func platformPowerShellBinary() (string, error) {
	for _, name := range platformPowerShellCandidates {
		if path, err := platformLookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("powershell: none of %s found on PATH; install PowerShell (pwsh) to run the powershell module on this platform", strings.Join(platformPowerShellCandidates, ", "))
}

func platformPowerShellArgs() []string {
	return []string{"-NoProfile", "-NonInteractive"}
}
