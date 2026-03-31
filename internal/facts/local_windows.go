//go:build windows

package facts

import "os"

func gatherLocalDisks() ([]DiskFacts, error) {
	// On Windows, disk facts are gathered via PowerShell through the target.
	// This path is only reached for a local Windows target, which uses the
	// same powershell path. Return empty to avoid duplication.
	return []DiskFacts{}, nil
}

func gatherLocalEnv() map[string]string {
	env := make(map[string]string)
	for _, e := range os.Environ() {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				env[e[:i]] = e[i+1:]
				break
			}
		}
	}
	return env
}
