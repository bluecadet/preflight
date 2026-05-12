package target

import "strings"

// windowsRemoteTempDir is the canonical staging directory used on every Windows
// target — controller-local, WinRM, and SSH alike. Keeping this in one place
// prevents drift; previously every transport returned the same literal from its
// own RemoteTempDir() method.
const windowsRemoteTempDir = `C:\Windows\Temp\preflight`

func normalizeOSFamily(name string) OSFamily {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "windows", "win32", "win32nt":
		return OSFamilyWindows
	case "linux":
		return OSFamilyLinux
	case "darwin", "macos", "macosx":
		return OSFamilyDarwin
	default:
		return OSFamilyUnknown
	}
}
