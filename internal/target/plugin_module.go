package target

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type pluginClient interface {
	Name() string
	Check(args map[string]any) (sdk.CheckResult, error)
	Apply(args map[string]any) (sdk.ApplyResult, error)
	Close() error
}

type pluginModule struct {
	name          string
	path          string
	newClient     func(context.Context, string) (pluginClient, error)
	mu            sync.Mutex
	client        pluginClient
	closeClientFn func(pluginClient) error
}

func (m *pluginModule) clone() *pluginModule {
	return &pluginModule{
		name:          m.name,
		path:          m.path,
		newClient:     m.newClient,
		closeClientFn: m.closeClientFn,
	}
}

// NewPluginModule adapts an executable plugin into the Module contract used by
// targets and the runner.
func NewPluginModule(name, path string) Module {
	return &pluginModule{
		name:      name,
		path:      path,
		newClient: func(ctx context.Context, path string) (pluginClient, error) { return sdk.NewClientContext(ctx, path) },
	}
}

func (m *pluginModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	client, err := m.getOrCreateClient(ctx)
	if err != nil {
		return false, fmt.Errorf("plugin %q: %w", m.name, err)
	}
	if name := client.Name(); name != "" && name != m.name {
		_ = m.Close()
		return false, fmt.Errorf("plugin %q reported logical name %q", m.name, name)
	}

	result, err := client.Check(params)
	if err != nil {
		m.resetClient(client)
		return false, fmt.Errorf("plugin %q check: %w", m.name, err)
	}
	return result.NeedsChange, nil
}

func (m *pluginModule) Apply(ctx context.Context, params map[string]any) error {
	client, err := m.getOrCreateClient(ctx)
	if err != nil {
		return fmt.Errorf("plugin %q: %w", m.name, err)
	}
	if name := client.Name(); name != "" && name != m.name {
		_ = m.Close()
		return fmt.Errorf("plugin %q reported logical name %q", m.name, name)
	}

	if _, err := client.Apply(params); err != nil {
		m.resetClient(client)
		return fmt.Errorf("plugin %q apply: %w", m.name, err)
	}
	return nil
}

func (m *pluginModule) Close() error {
	m.mu.Lock()
	client := m.client
	m.client = nil
	closeClientFn := m.closeClientFn
	m.mu.Unlock()

	if client == nil {
		return nil
	}
	if closeClientFn != nil {
		return closeClientFn(client)
	}
	return client.Close()
}

func (m *pluginModule) getOrCreateClient(ctx context.Context) (pluginClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		return m.client, nil
	}

	newClient := m.newClient
	if newClient == nil {
		newClient = func(ctx context.Context, path string) (pluginClient, error) { return sdk.NewClientContext(ctx, path) }
	}

	client, err := newClient(ctx, m.path)
	if err != nil {
		return nil, err
	}
	m.client = client
	return client, nil
}

func (m *pluginModule) resetClient(client pluginClient) {
	m.mu.Lock()
	if m.client != client {
		m.mu.Unlock()
		return
	}
	m.client = nil
	closeClientFn := m.closeClientFn
	m.mu.Unlock()

	if closeClientFn != nil {
		_ = closeClientFn(client)
		return
	}
	_ = client.Close()
}
