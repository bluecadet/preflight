package facts

import (
	"encoding/json"
	"fmt"
)

// windowsDrive is the JSON shape returned by PowerShell's Get-PSDrive.
type windowsDrive struct {
	Name string  `json:"Name"`
	Used float64 `json:"Used"`
	Free float64 `json:"Free"`
}

// parseWindowsDrives parses the JSON output of:
//
//	Get-PSDrive -PSProvider FileSystem | Select Name,Used,Free | ConvertTo-Json
//
// PowerShell may return a single object or an array depending on the number of
// drives present. Both forms are handled.
func parseWindowsDrives(jsonData []byte) ([]DiskFacts, error) {
	// Try array form first.
	var drives []windowsDrive
	if err := json.Unmarshal(jsonData, &drives); err != nil {
		// Fall back to single-object form.
		var single windowsDrive
		if err2 := json.Unmarshal(jsonData, &single); err2 != nil {
			return nil, fmt.Errorf("facts: parse windows drives: %w", err)
		}
		drives = []windowsDrive{single}
	}

	result := make([]DiskFacts, 0, len(drives))
	for _, d := range drives {
		totalBytes := d.Used + d.Free
		const gb = 1 << 30
		result = append(result, DiskFacts{
			Path:    d.Name + ":",
			TotalGB: totalBytes / gb,
			FreeGB:  d.Free / gb,
			UsedGB:  d.Used / gb,
		})
	}
	return result, nil
}
