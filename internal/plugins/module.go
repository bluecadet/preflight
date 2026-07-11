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
	Check(ctx context.Context, args map[string]any, out sdk.OutputFunc) (sdk.CheckResult, error)
	Apply(ctx context.Context, args map[string]any, out sdk.OutputFunc) (sdk.ApplyResult, error)
	Close() error
}

// Plugin adapts an executable plugin into target.Module. It is created unbound
// (path only) during BuildRegistry; a target binds it to its TargetOps backend
// via BindTarget. Once bound, the lazily-created client carries protocol_version
// and the enriched TargetInfo at initialize, and the plugin's handle-op
// requests (RunCommand/PutFile/GetFile) flow through the bound ops backend —
// including against the local target.
//
// Plugin satisfies target.PluggableModule. It forwards Message and streams
// output (the silent drops of the v0 adapter are fixed): Check/Apply use the
// streaming SDK calls so plugin output notifications reach the runner.
type Plugin struct {
	name      string
	path      string
	ops       target.TargetOps
	newClient func(ctx context.Context, path string, info sdk.TargetInfo, hs sdk.HandleServer) (pluginClient, error)

	mu     sync.Mutex
	client pluginClient
}

// NewModule adapts an executable plugin into an unbound target.Module. A target
// must BindTarget it before use.
func NewModule(name, path string) target.Module {
	return &Plugin{
		name:      name,
		path:      path,
		newClient: defaultNewClient,
	}
}

// defaultNewClient is the production client factory: it spawns the plugin and
// performs the v1 initialize handshake (protocol_version + TargetInfo), binding
// handle-op requests to hs. *sdk.Client satisfies pluginClient directly.
func defaultNewClient(ctx context.Context, path string, info sdk.TargetInfo, hs sdk.HandleServer) (pluginClient, error) {
	return sdk.NewClientContext(ctx, path, info, hs)
}

// BindTarget returns a fresh Plugin bound to the given target ops backend. The
// returned Module owns its own (lazily-created) client state; the receiver
// stays unbound so another target can bind it independently.
func (m *Plugin) BindTarget(ops target.TargetOps) target.Module {
	return &Plugin{
		name:      m.name,
		path:      m.path,
		ops:       ops,
		newClient: m.newClient,
	}
}

// PluginPath returns the filesystem path to the backing plugin executable.
func (m *Plugin) PluginPath() string {
	return m.path
}

func (m *Plugin) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	client, err := m.getOrCreateClient(ctx)
	if err != nil {
		return target.CheckResult{}, err
	}
	if name := client.Name(); name != "" && name != m.name {
		_ = m.Close()
		return target.CheckResult{}, fmt.Errorf("plugin %q reported logical name %q", m.name, name)
	}

	res, err := client.Check(ctx, params, sdk.OutputFunc(out))
	if err != nil {
		m.resetClient(client)
		return target.CheckResult{}, wrapPluginErr(m.name, "check", err)
	}
	return target.CheckResult{NeedsChange: res.NeedsChange, Message: res.Message}, nil
}

func (m *Plugin) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	client, err := m.getOrCreateClient(ctx)
	if err != nil {
		return target.ApplyResult{}, err
	}
	if name := client.Name(); name != "" && name != m.name {
		_ = m.Close()
		return target.ApplyResult{}, fmt.Errorf("plugin %q reported logical name %q", m.name, name)
	}

	res, err := client.Apply(ctx, params, sdk.OutputFunc(out))
	if err != nil {
		m.resetClient(client)
		return target.ApplyResult{}, wrapPluginErr(m.name, "apply", err)
	}
	return target.ApplyResult{Message: res.Message}, nil
}

// wrapPluginErr returns a typed plugin_protocol ModuleSupportError when err is
// a protocol-version failure, otherwise a generic wrapped plugin error. op is
// the call site ("check", "apply", or "" for client creation).
func wrapPluginErr(name, op string, err error) error {
	if sdk.IsProtocolError(err) {
		return target.NewPluginProtocolError(name, err.Error())
	}
	if op == "" {
		return fmt.Errorf("plugin %q: %w", name, err)
	}
	return fmt.Errorf("plugin %q %s: %w", name, op, err)
}

func (m *Plugin) Close() error {
	m.mu.Lock()
	client := m.client
	m.client = nil
	m.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (m *Plugin) getOrCreateClient(ctx context.Context) (pluginClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		return m.client, nil
	}
	if m.ops == nil {
		return nil, fmt.Errorf("plugin %q: not bound to a target", m.name)
	}
	info, err := m.ops.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: target info: %w", m.name, err)
	}
	sdkInfo := toSDKTargetInfo(info)
	client, err := m.newClient(ctx, m.path, sdkInfo, &opsHandleServer{ops: m.ops})
	if err != nil {
		return nil, wrapPluginErr(m.name, "", err)
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
	m.mu.Unlock()
	_ = client.Close()
}

// opsHandleServer adapts a target.TargetOps to the sdk.HandleServer interface
// so the SDK Client can dispatch plugin handle-op requests to the target
// backend.
type opsHandleServer struct {
	ops target.TargetOps
}

func (o *opsHandleServer) RunCommand(ctx context.Context, script string) (sdk.CommandResult, error) {
	res, err := o.ops.Exec(ctx, script)
	if err != nil {
		return sdk.CommandResult{}, err
	}
	return sdk.CommandResult{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, nil
}

func (o *opsHandleServer) PutFile(ctx context.Context, path string, data []byte) error {
	return o.ops.PutFile(ctx, path, data)
}

func (o *opsHandleServer) GetFile(ctx context.Context, path string) ([]byte, error) {
	return o.ops.GetFile(ctx, path)
}

// toSDKTargetInfo converts the internal target.TargetInfo to the wire
// TargetInfo delivered to a plugin at initialize. runtime_kind is derived from
// the OS family (windows → windows-powershell, else posix-shell), matching the
// transport runtime kinds. Absent POSIX signals stay empty strings.
func toSDKTargetInfo(info target.TargetInfo) sdk.TargetInfo {
	runtimeKind := "posix-shell"
	if info.OSFamily == target.OSFamilyWindows {
		runtimeKind = "windows-powershell"
	}
	return sdk.TargetInfo{
		Family:         string(info.OSFamily),
		Name:           info.OSName,
		Version:        info.OSVersion,
		Arch:           info.Arch,
		Hostname:       info.Hostname,
		PackageManager: info.PackageManager,
		Init:           info.Init,
		RuntimeKind:    runtimeKind,
	}
}
