package target

import "strings"

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
