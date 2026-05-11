package module

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type FileParams struct {
	Dest    string `param:"dest,required"`
	Ensure  string `param:"ensure" default:"present"`
	Src     string `param:"src"`
	Content string `param:"content"`
}

type FileModule struct{}

func (m *FileModule) Check(_ context.Context, params map[string]any) (bool, error) {
	if err := RejectParams("file", params, "owner", "permissions"); err != nil {
		return false, err
	}
	var p FileParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	hasContent, err := validateFileContentParams("file", params, p.Src)
	if err != nil {
		return false, err
	}

	info, statErr := os.Stat(p.Dest)

	return EnsureCheck("file", p.Ensure,
		func() (bool, error) {
			if os.IsNotExist(statErr) {
				return true, nil
			}
			if statErr != nil {
				return false, fmt.Errorf("file: stat %q: %w", p.Dest, statErr)
			}
			if info.IsDir() {
				return false, fmt.Errorf("file: %q is a directory, not a file", p.Dest)
			}
			if p.Src != "" {
				srcHash, err := hashFile(p.Src)
				if err != nil {
					return false, fmt.Errorf("file: hash src %q: %w", p.Src, err)
				}
				dstHash, err := hashFile(p.Dest)
				if err != nil {
					return false, fmt.Errorf("file: hash dest %q: %w", p.Dest, err)
				}
				return srcHash != dstHash, nil
			}
			if hasContent {
				dstHash, err := hashFile(p.Dest)
				if err != nil {
					return false, fmt.Errorf("file: hash dest %q: %w", p.Dest, err)
				}
				return hashBytes([]byte(p.Content)) != dstHash, nil
			}
			return false, nil
		},
		func() (bool, error) {
			if os.IsNotExist(statErr) {
				return false, nil
			}
			if statErr != nil {
				return false, fmt.Errorf("file: stat %q: %w", p.Dest, statErr)
			}
			return true, nil
		},
	)
}

func (m *FileModule) Apply(_ context.Context, params map[string]any) error {
	if err := RejectParams("file", params, "owner", "permissions"); err != nil {
		return err
	}
	var p FileParams
	if err := Decode(params, &p); err != nil {
		return err
	}
	hasContent, err := validateFileContentParams("file", params, p.Src)
	if err != nil {
		return err
	}

	return EnsureApply("file", p.Ensure,
		func() error {
			if p.Src != "" {
				return copyFile(p.Src, p.Dest)
			}
			if hasContent {
				return writeFileContent(p.Dest, p.Content)
			}
			if err := ensureParentDir(p.Dest); err != nil {
				return err
			}
			f, err := os.OpenFile(p.Dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return fmt.Errorf("file: create %q: %w", p.Dest, err)
			}
			return f.Close()
		},
		func() error {
			if err := os.Remove(p.Dest); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("file: remove %q: %w", p.Dest, err)
			}
			return nil
		},
	)
}

func validateFileContentParams(module string, params map[string]any, src string) (bool, error) {
	_, hasContent := params["content"]
	if src != "" && hasContent {
		return false, fmt.Errorf("%s: src and content are mutually exclusive", module)
	}
	return hasContent, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
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

	if err := ensureParentDir(dst); err != nil {
		return err
	}

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

func writeFileContent(dst, content string) error {
	if err := ensureParentDir(dst); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		return fmt.Errorf("file: write %q: %w", dst, err)
	}
	return nil
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("file: mkdir parent %q: %w", dir, err)
	}
	return nil
}
