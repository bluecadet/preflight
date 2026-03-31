package target

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type pluginModule struct {
	name string
	path string
}

// NewPluginModule adapts an executable plugin into the Module contract used by
// targets and the runner.
func NewPluginModule(name, path string) Module {
	return &pluginModule{name: name, path: path}
}

func (m *pluginModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	client, err := sdk.NewClientContext(ctx, m.path)
	if err != nil {
		return false, fmt.Errorf("plugin %q: %w", m.name, err)
	}
	defer func() { _ = client.Close() }()

	result, err := client.Check(params)
	if err != nil {
		return false, fmt.Errorf("plugin %q check: %w", m.name, err)
	}
	return result.Changed, nil
}

func (m *pluginModule) Apply(ctx context.Context, params map[string]any) error {
	client, err := sdk.NewClientContext(ctx, m.path)
	if err != nil {
		return fmt.Errorf("plugin %q: %w", m.name, err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.Apply(params); err != nil {
		return fmt.Errorf("plugin %q apply: %w", m.name, err)
	}
	return nil
}
