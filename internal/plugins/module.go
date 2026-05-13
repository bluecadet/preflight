package plugins

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// pluginClient is the subset of sdk.Client behaviour exercised by Plugin.
// Defined as an interface so tests can substitute a fake without spawning a
// real plugin subprocess.
type pluginClient interface {
	Name() string
	Check(args map[string]any) (sdk.CheckResult, error)
	Apply(args map[string]any) (sdk.ApplyResult, error)
	Close() error
}

// Plugin adapts an executable plugin into target.Module. Each Plugin owns at
// most one live client at a time; the client is lazily created on first Check
// or Apply and reused across calls until reset or Close.
//
// Plugin satisfies target.PluggableModule: LocalTarget clones it per-instance
// so each target keeps its own plugin client, and other transports use the
// interface to report a friendlier "use local or a staged bundle" error when
// a plugin is invoked over a transport that cannot delegate.
type Plugin struct {
	name          string
	path          string
	newClient     func(context.Context, string) (pluginClient, error)
	mu            sync.Mutex
	client        pluginClient
	closeClientFn func(pluginClient) error
}

// NewModule adapts an executable plugin into target.Module.
func NewModule(name, path string) target.Module {
	return &Plugin{
		name:      name,
		path:      path,
		newClient: func(ctx context.Context, path string) (pluginClient, error) { return sdk.NewClientContext(ctx, path) },
	}
}

// CloneModule returns a fresh Plugin sharing the same name, path, and factory
// but no live client. Used by LocalTarget so each target instance gets its
// own plugin subprocess state.
func (m *Plugin) CloneModule() target.Module {
	return &Plugin{
		name:          m.name,
		path:          m.path,
		newClient:     m.newClient,
		closeClientFn: m.closeClientFn,
	}
}

// PluginPath returns the filesystem path to the backing plugin executable.
func (m *Plugin) PluginPath() string {
	return m.path
}

func (m *Plugin) Check(ctx context.Context, params map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	client, err := m.getOrCreateClient(ctx)
	if err != nil {
		return target.CheckResult{}, fmt.Errorf("plugin %q: %w", m.name, err)
	}
	if name := client.Name(); name != "" && name != m.name {
		_ = m.Close()
		return target.CheckResult{}, fmt.Errorf("plugin %q reported logical name %q", m.name, name)
	}

	result, err := client.Check(params)
	if err != nil {
		m.resetClient(client)
		return target.CheckResult{}, fmt.Errorf("plugin %q check: %w", m.name, err)
	}
	return target.CheckResult{NeedsChange: result.NeedsChange}, nil
}

func (m *Plugin) Apply(ctx context.Context, params map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	client, err := m.getOrCreateClient(ctx)
	if err != nil {
		return target.ApplyResult{}, fmt.Errorf("plugin %q: %w", m.name, err)
	}
	if name := client.Name(); name != "" && name != m.name {
		_ = m.Close()
		return target.ApplyResult{}, fmt.Errorf("plugin %q reported logical name %q", m.name, name)
	}

	if _, err := client.Apply(params); err != nil {
		m.resetClient(client)
		return target.ApplyResult{}, fmt.Errorf("plugin %q apply: %w", m.name, err)
	}
	return target.ApplyResult{}, nil
}

func (m *Plugin) Close() error {
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

func (m *Plugin) getOrCreateClient(ctx context.Context) (pluginClient, error) {
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

func (m *Plugin) resetClient(client pluginClient) {
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
