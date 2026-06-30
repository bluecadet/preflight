//go:build !windows

package action

import (
	"os"

	"github.com/google/renameio/v2"
)

// WriteFileAtomically writes data to path atomically using a temporary file
// and rename.
func WriteFileAtomically(path string, data []byte, perm uint32) error {
	return renameio.WriteFile(path, data, os.FileMode(perm))
}
