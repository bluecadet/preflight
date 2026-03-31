package module

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// FileModule manages a file at a destination path.
// Params:
//   - dest (required): destination path
//   - src: source path to copy from
//   - ensure: "present" (default) or "absent"
//   - owner: (future) file owner
//   - permissions: (future) file permissions
type FileModule struct{}

func (m *FileModule) Check(_ context.Context, params map[string]any) (bool, error) {
	dest, err := paramStringRequired(params, "dest")
	if err != nil {
		return false, err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return false, err
	}
	src, err := paramString(params, "src", "")
	if err != nil {
		return false, err
	}

	info, statErr := os.Stat(dest)

	switch ensure {
	case "absent":
		if os.IsNotExist(statErr) {
			return false, nil // already gone
		}
		if statErr != nil {
			return false, fmt.Errorf("file: stat %q: %w", dest, statErr)
		}
		return true, nil // exists, needs removal

	case "present":
		if os.IsNotExist(statErr) {
			return true, nil // needs creation
		}
		if statErr != nil {
			return false, fmt.Errorf("file: stat %q: %w", dest, statErr)
		}
		if info.IsDir() {
			return false, fmt.Errorf("file: %q is a directory, not a file", dest)
		}
		// If src provided, compare hashes.
		if src != "" {
			srcHash, err := hashFile(src)
			if err != nil {
				return false, fmt.Errorf("file: hash src %q: %w", src, err)
			}
			dstHash, err := hashFile(dest)
			if err != nil {
				return false, fmt.Errorf("file: hash dest %q: %w", dest, err)
			}
			return srcHash != dstHash, nil
		}
		return false, nil

	default:
		return false, fmt.Errorf("file: unknown ensure value %q (want present|absent)", ensure)
	}
}

func (m *FileModule) Apply(_ context.Context, params map[string]any) error {
	dest, err := paramStringRequired(params, "dest")
	if err != nil {
		return err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return err
	}
	src, err := paramString(params, "src", "")
	if err != nil {
		return err
	}

	switch ensure {
	case "absent":
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("file: remove %q: %w", dest, err)
		}
		return nil

	case "present":
		if src != "" {
			return copyFile(src, dest)
		}
		// Create empty file.
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("file: create %q: %w", dest, err)
		}
		return f.Close()

	default:
		return fmt.Errorf("file: unknown ensure value %q (want present|absent)", ensure)
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("file: open src %q: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("file: open dest %q: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("file: copy %q → %q: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("file: close dest %q: %w", dst, err)
	}
	return nil
}
