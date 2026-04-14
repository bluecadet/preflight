//go:build windows

package fsutil

import "os"

func WriteFileAtomic(path string, data []byte, perm uint32) error {
	return os.WriteFile(path, data, os.FileMode(perm))
}
