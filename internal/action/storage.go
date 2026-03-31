package action

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// actionDirForRef returns the on-disk directory for a cached action ref.
// It returns an error if the resolved path escapes baseDir, which would
// indicate a path traversal attempt.
func actionDirForRef(baseDir, ref string) (string, error) {
	resolved := filepath.Join(baseDir, filepath.FromSlash(ref))
	if !isSubPath(baseDir, resolved) {
		return "", fmt.Errorf("action ref %q resolves outside cache directory", ref)
	}
	return resolved, nil
}

func actionFileForRef(baseDir, ref string) (string, error) {
	dir, err := actionDirForRef(baseDir, ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "action.yml"), nil
}

// isSubPath reports whether target is within (or equal to) base after cleaning
// both paths.
func isSubPath(base, target string) bool {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	return target == base || strings.HasPrefix(target, base+string(filepath.Separator))
}

func loadActionFromCache(cacheDir, ref string) (*Action, error) {
	actionFile, err := actionFileForRef(cacheDir, ref)
	if err != nil {
		return nil, fmt.Errorf("read cached action %q: %w", ref, err)
	}
	data, err := os.ReadFile(actionFile)
	if err != nil {
		if errorsIsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cached action %q: %w", ref, err)
	}
	action, err := ParseAction(data)
	if err != nil {
		return nil, fmt.Errorf("parse cached action %q: %w", ref, err)
	}
	return action, nil
}

func loadActionFromDir(dir string) (*Action, error) {
	data, err := os.ReadFile(filepath.Join(dir, "action.yml"))
	if err != nil {
		return nil, err
	}
	return ParseAction(data)
}

func copyDir(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dstDir, err)
	}

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy %q: symbolic links are not supported", path)
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("rel %q: %w", path, err)
		}
		dstPath := filepath.Join(dstDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		if err := copyFile(path, dstPath, info.Mode()); err != nil {
			return err
		}
		return nil
	})
}

func copyFile(srcPath, dstPath string, mode fs.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %q: %w", srcPath, err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(dstPath), err)
	}

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return fmt.Errorf("create %q: %w", dstPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy %q -> %q: %w", srcPath, dstPath, err)
	}
	return nil
}
