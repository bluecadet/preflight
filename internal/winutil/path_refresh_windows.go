//go:build windows

package winutil

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// RefreshProcessPath re-reads PATH and PATHEXT from the Machine and User
// registry hives and applies them to the current process. This is needed
// in two situations:
//
//   - At binary startup, because the parent shell that launched preflight
//     may itself have a stale PATH from before earlier installs.
//   - After running an installer (winget, MSI) so subsequent child
//     powershell.exe processes inherit binaries added during this run.
func RefreshProcessPath() {
	const machineEnv = `System\CurrentControlSet\Control\Session Manager\Environment`
	machine := readRegistryEnv(registry.LOCAL_MACHINE, machineEnv, "Path")
	user := readRegistryEnv(registry.CURRENT_USER, `Environment`, "Path")
	if combined := joinPathSegments(machine, user); combined != "" {
		_ = os.Setenv("PATH", combined)
	}
	if ext := readRegistryEnv(registry.LOCAL_MACHINE, machineEnv, "PATHEXT"); ext != "" {
		_ = os.Setenv("PATHEXT", ext)
	}
}

func readRegistryEnv(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	val, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return expandEnvStrings(val)
}

func expandEnvStrings(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	src, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return s
	}
	buf := make([]uint16, 1024)
	n, err := windows.ExpandEnvironmentStrings(src, &buf[0], uint32(len(buf)))
	if err != nil {
		return s
	}
	if int(n) > len(buf) {
		buf = make([]uint16, n)
		n, err = windows.ExpandEnvironmentStrings(src, &buf[0], uint32(len(buf)))
		if err != nil {
			return s
		}
	}
	return windows.UTF16ToString(buf[:n])
}

func joinPathSegments(machine, user string) string {
	machine = strings.Trim(strings.TrimSpace(machine), ";")
	user = strings.Trim(strings.TrimSpace(user), ";")
	switch {
	case machine == "" && user == "":
		return ""
	case machine == "":
		return user
	case user == "":
		return machine
	default:
		return machine + ";" + user
	}
}
