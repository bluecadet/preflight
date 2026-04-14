package plugins

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// LoadedPlugin is one plugin executable registered into the runtime module map.
type LoadedPlugin struct {
	Name    string
	Path    string
	Source  string
	Version string
}

// Options controls plugin discovery and registration.
type Options struct {
	BinaryDir              string
	WorkingDir             string
	PreferredDirs          []string
	ExclusivePreferredDirs bool
}

// BuildRegistry merges built-in modules with discovered plugins. Plugin names
// may not shadow built-ins and duplicate plugin names are rejected.
func BuildRegistry(base target.ModuleRegistry, opts Options) (target.ModuleRegistry, []LoadedPlugin, error) {
	registry := make(target.ModuleRegistry, len(base))
	maps.Copy(registry, base)

	discovered, err := sdk.Scan(sdk.DiscoveryOptions{
		BinaryDir:           opts.BinaryDir,
		WorkingDir:          opts.WorkingDir,
		PreferredDirs:       opts.PreferredDirs,
		DisableFallbackDirs: opts.ExclusivePreferredDirs,
	})
	if err != nil {
		return nil, nil, err
	}

	seenPlugins := make(map[string]string)
	loaded := make([]LoadedPlugin, 0, len(discovered))
	for _, plugin := range discovered {
		if _, exists := registry[plugin.Name]; exists {
			if _, builtin := base[plugin.Name]; builtin {
				return nil, nil, fmt.Errorf("plugin %q conflicts with built-in module name", plugin.Name)
			}
		}
		if existingPath, duplicate := seenPlugins[plugin.Name]; duplicate {
			return nil, nil, fmt.Errorf("plugin %q discovered more than once (%s, %s)", plugin.Name, existingPath, plugin.Path)
		}
		seenPlugins[plugin.Name] = plugin.Path
		registry[plugin.Name] = target.NewPluginModule(plugin.Name, plugin.Path)
		loaded = append(loaded, LoadedPlugin{
			Name:   plugin.Name,
			Path:   plugin.Path,
			Source: plugin.Source,
		})
		slog.Debug("plugin registered", "name", plugin.Name, "path", plugin.Path)
	}
	slog.Debug("plugin discovery complete", "discovered", len(discovered), "loaded", len(loaded))

	slices.SortFunc(loaded, func(a, b LoadedPlugin) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	return registry, loaded, nil
}
