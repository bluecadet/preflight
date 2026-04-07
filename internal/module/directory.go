package module

import (
	"context"
	"fmt"
	"os"
)

// DirectoryModule manages a directory at a given path.
// Params:
//   - path (required): directory path
//   - ensure: "present" (default) or "absent"
//   - owner: (future) directory owner
//   - permissions: (future) directory permissions
type DirectoryModule struct{}

func (m *DirectoryModule) Check(_ context.Context, params map[string]any) (bool, error) {
	path, err := paramStringRequired(params, "path")
	if err != nil {
		return false, err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return false, err
	}
	if _, ok := params["owner"]; ok {
		return false, fmt.Errorf("directory: owner is not supported on this platform")
	}
	if _, ok := params["permissions"]; ok {
		return false, fmt.Errorf("directory: permissions is not supported on this platform")
	}

	info, statErr := os.Stat(path)

	switch ensure {
	case "absent":
		if os.IsNotExist(statErr) {
			return false, nil // already gone
		}
		if statErr != nil {
			return false, fmt.Errorf("directory: stat %q: %w", path, statErr)
		}
		return true, nil // exists, needs removal

	case "present":
		if os.IsNotExist(statErr) {
			return true, nil // needs creation
		}
		if statErr != nil {
			return false, fmt.Errorf("directory: stat %q: %w", path, statErr)
		}
		if !info.IsDir() {
			return false, fmt.Errorf("directory: %q exists but is not a directory", path)
		}
		return false, nil // already a directory

	default:
		return false, fmt.Errorf("directory: unknown ensure value %q (want present|absent)", ensure)
	}
}

func (m *DirectoryModule) Apply(_ context.Context, params map[string]any) error {
	path, err := paramStringRequired(params, "path")
	if err != nil {
		return err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return err
	}

	switch ensure {
	case "absent":
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("directory: remove %q: %w", path, err)
		}
		return nil

	case "present":
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("directory: mkdir %q: %w", path, err)
		}
		return nil

	default:
		return fmt.Errorf("directory: unknown ensure value %q (want present|absent)", ensure)
	}
}
