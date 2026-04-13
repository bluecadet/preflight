package module

import (
	"context"
	"fmt"
	"os"
)

type DirectoryParams struct {
	Path   string `param:"path,required"`
	Ensure string `param:"ensure" default:"present"`
}

type DirectoryModule struct{}

func (m *DirectoryModule) Check(_ context.Context, params map[string]any) (bool, error) {
	if err := RejectParams("directory", params, "owner", "permissions"); err != nil {
		return false, err
	}
	var p DirectoryParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}

	info, statErr := os.Stat(p.Path)

	return EnsureCheck("directory", p.Ensure,
		func() (bool, error) {
			if os.IsNotExist(statErr) {
				return true, nil
			}
			if statErr != nil {
				return false, fmt.Errorf("directory: stat %q: %w", p.Path, statErr)
			}
			if !info.IsDir() {
				return false, fmt.Errorf("directory: %q exists but is not a directory", p.Path)
			}
			return false, nil
		},
		func() (bool, error) {
			if os.IsNotExist(statErr) {
				return false, nil
			}
			if statErr != nil {
				return false, fmt.Errorf("directory: stat %q: %w", p.Path, statErr)
			}
			return true, nil
		},
	)
}

func (m *DirectoryModule) Apply(_ context.Context, params map[string]any) error {
	if err := RejectParams("directory", params, "owner", "permissions"); err != nil {
		return err
	}
	var p DirectoryParams
	if err := Decode(params, &p); err != nil {
		return err
	}

	return EnsureApply("directory", p.Ensure,
		func() error {
			if err := os.MkdirAll(p.Path, 0755); err != nil {
				return fmt.Errorf("directory: mkdir %q: %w", p.Path, err)
			}
			return nil
		},
		func() error {
			if err := os.RemoveAll(p.Path); err != nil {
				return fmt.Errorf("directory: remove %q: %w", p.Path, err)
			}
			return nil
		},
	)
}
