//go:build !windows

package fsutil

import (
	"os"

	"github.com/google/renameio/v2"
)

func WriteFileAtomic(path string, data []byte, perm uint32) error {
	return renameio.WriteFile(path, data, os.FileMode(perm))
}
