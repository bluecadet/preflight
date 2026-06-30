//go:build windows

package action

import "os"

// WriteFileAtomically writes data to path atomically using a temporary file
// and rename. On Windows, atomic rename is not reliably supported so this
// falls back to a direct write.
func WriteFileAtomically(path string, data []byte, perm uint32) error {
	return os.WriteFile(path, data, os.FileMode(perm))
}