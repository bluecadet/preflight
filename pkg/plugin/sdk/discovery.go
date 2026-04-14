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

// DiscoveryOptions controls plugin scan order.
type DiscoveryOptions struct {
	BinaryDir           string
	WorkingDir          string
	PreferredDirs       []string
	DisableFallbackDirs bool
}

// DiscoveredPlugin is one plugin executable found during scanning.
type DiscoveredPlugin struct {
	Name   string
	Path   string
	Source string
}

// PluginStatus describes a discovered plugin plus initialization status.
type PluginStatus struct {
	Name         string
	Path         string
	Source       string
	Version      string
	Initialized  bool
	ErrorMessage string
}

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
	plugins, err := Scan(DiscoveryOptions{BinaryDir: binaryDir})
	if err != nil {
		return nil, err
	}

	paths := make(map[string]string, len(plugins))
	for _, plugin := range plugins {
		if _, exists := paths[plugin.Name]; exists {
			continue
		}
		paths[plugin.Name] = plugin.Path
	}

	return paths, nil
}

// Scan returns every matching plugin executable in scan order. PreferredDirs
// are searched before the default binary/home/cwd directories.
func Scan(opts DiscoveryOptions) ([]DiscoveredPlugin, error) {
	dirs, err := discoverDirs(opts)
	if err != nil {
		return nil, err
	}

	var plugins []DiscoveredPlugin
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

			absPath := filepath.Join(dir, entry.Name())
			plugins = append(plugins, DiscoveredPlugin{
				Name:   name,
				Path:   absPath,
				Source: dir,
			})
		}
	}

	return plugins, nil
}

// Inspect initializes each discovered plugin and returns its reported version
// or the initialization failure.
func Inspect(opts DiscoveryOptions) ([]PluginStatus, error) {
	plugins, err := Scan(opts)
	if err != nil {
		return nil, err
	}

	statuses := make([]PluginStatus, 0, len(plugins))
	for _, plugin := range plugins {
		status := PluginStatus{
			Name:   plugin.Name,
			Path:   plugin.Path,
			Source: plugin.Source,
		}

		client, err := NewClient(plugin.Path)
		if err != nil {
			status.ErrorMessage = err.Error()
			statuses = append(statuses, status)
			continue
		}

		status.Initialized = true
		status.Version = client.Version()
		if client.Name() != "" {
			status.Name = client.Name()
		}
		_ = client.Close()

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// InspectPlugin initializes a single plugin executable and returns its status.
func InspectPlugin(path, source string) PluginStatus {
	status := PluginStatus{Path: path, Source: source}
	name, ok := pluginName(filepath.Base(path))
	if ok {
		status.Name = name
	}

	client, err := NewClient(path)
	if err != nil {
		status.ErrorMessage = err.Error()
		return status
	}
	defer func() { _ = client.Close() }()

	status.Initialized = true
	status.Version = client.Version()
	if client.Name() != "" {
		status.Name = client.Name()
	}
	return status
}

// discoverDirs returns the ordered list of directories to scan.
func discoverDirs(opts DiscoveryOptions) ([]string, error) {
	dirs := []string{}

	seen := make(map[string]struct{})
	appendDir := func(dir string) {
		if dir == "" {
			return
		}
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		dirs = append(dirs, clean)
	}

	for _, dir := range opts.PreferredDirs {
		appendDir(dir)
	}

	if opts.DisableFallbackDirs {
		return dirs, nil
	}

	if opts.BinaryDir != "" {
		appendDir(opts.BinaryDir)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("discover plugins: %w", err)
	}
	appendDir(filepath.Join(home, ".preflight", "plugins"))

	cwd := opts.WorkingDir
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("discover plugins: %w", err)
		}
	}
	appendDir(filepath.Join(cwd, "plugins"))

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
