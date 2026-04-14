//go:build windows

package facts

func gatherLocalDisks() ([]DiskFacts, error) {
	// On Windows, disk facts are gathered via PowerShell through the target.
	// This path is only reached for a local Windows target, which uses the
	// same powershell path. Return empty to avoid duplication.
	return []DiskFacts{}, nil
}
