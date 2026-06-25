package output

import (
	"crypto/rand"
	"fmt"
	"time"
)

// RunID generates a unique run identifier in the format YYYYMMDD-HHMMSS-XXXX
// where XXXX is 4 random hex chars.
func RunID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	now := time.Now()
	return fmt.Sprintf("%s-%04x",
		now.Format("20060102-150405"),
		b,
	)
}

// RunDir returns the relative path for a run directory under .preflight/runs/.
func RunDir(runID string) string {
	return fmt.Sprintf(".preflight/runs/%s", runID)
}