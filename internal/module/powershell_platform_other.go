//go:build !windows

package module

func platformPowerShellBinary() string {
	return "powershell"
}

func platformPowerShellArgs() []string {
	return []string{"-NoProfile", "-NonInteractive"}
}
