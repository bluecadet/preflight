package sdk

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// pluginPrefix is the filename prefix for all preflight plugins.
const pluginPrefix = "preflight-plugin-"

// Discover scans well-known directories for preflight plugins and returns a map
// of plugin name → absolute executable path.
//
// Scan order (first match wins):
//  1. binaryDir — directory alongside the preflight binary
//  2. ~/.preflight/plugins/
//  3. ./plugins/ — relative to the current working directory
//
// On Windows, executables must have the ".exe" suffix.
// On all other platforms, executables have no required suffix.
func Discover(binaryDir string) (map[string]string, error) {
	dirs, err := discoverDirs(binaryDir)
	if err != nil {
		return nil, err
	}

	plugins := make(map[string]string)

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Non-existent or unreadable dirs are silently skipped.
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name, ok := pluginName(entry.Name())
			if !ok {
				continue
			}

			// First match wins — don't overwrite an earlier discovery.
			if _, exists := plugins[name]; exists {
				continue
			}

			absPath := filepath.Join(dir, entry.Name())
			plugins[name] = absPath
		}
	}

	return plugins, nil
}

// discoverDirs returns the ordered list of directories to scan.
func discoverDirs(binaryDir string) ([]string, error) {
	dirs := []string{}

	if binaryDir != "" {
		dirs = append(dirs, binaryDir)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("discover plugins: %w", err)
	}
	dirs = append(dirs, filepath.Join(home, ".preflight", "plugins"))

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("discover plugins: %w", err)
	}
	dirs = append(dirs, filepath.Join(cwd, "plugins"))

	return dirs, nil
}

// pluginName extracts the logical plugin name from a filename, or returns
// ("", false) if the filename is not a valid plugin executable.
//
// Valid filenames:
//   - preflight-plugin-<name>       (Unix)
//   - preflight-plugin-<name>.exe   (Windows)
func pluginName(filename string) (string, bool) {
	isWindows := runtime.GOOS == "windows"

	if isWindows {
		if !strings.HasSuffix(filename, ".exe") {
			return "", false
		}
		filename = strings.TrimSuffix(filename, ".exe")
	}

	if !strings.HasPrefix(filename, pluginPrefix) {
		return "", false
	}

	name := strings.TrimPrefix(filename, pluginPrefix)
	if name == "" {
		return "", false
	}

	return name, true
}
