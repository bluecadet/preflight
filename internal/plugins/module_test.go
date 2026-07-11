package plugins

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type fakePluginClient struct {
	checkCalls  int
	applyCalls  int
	closeCalls  int
	needsChange bool
	name        string
	checkMsg    string
	applyMsg    string
	outputLines []string
}

func (c *fakePluginClient) Check(_ context.Context, _ map[string]any, out sdk.OutputFunc) (sdk.CheckResult, error) {
	c.checkCalls++
	for _, line := range c.outputLines {
		if out != nil {
			out(line)
		}
	}
	return sdk.CheckResult{NeedsChange: c.needsChange, Message: c.checkMsg}, nil
}

func (c *fakePluginClient) Apply(_ context.Context, _ map[string]any, out sdk.OutputFunc) (sdk.ApplyResult, error) {
	c.applyCalls++
	for _, line := range c.outputLines {
		if out != nil {
			out(line)
		}
	}
	return sdk.ApplyResult{Message: c.applyMsg}, nil
}

func (c *fakePluginClient) Close() error {
	c.closeCalls++
	return nil
}

func (c *fakePluginClient) Name() string { return c.name }

// fakeOps is a minimal TargetOps for adapter tests.
type fakeOps struct {
	info target.TargetInfo
}

func (f *fakeOps) Exec(context.Context, string) (target.ExecResult, error) {
	return target.ExecResult{}, nil
}
func (f *fakeOps) PutFile(context.Context, string, []byte) error   { return nil }
func (f *fakeOps) GetFile(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeOps) Info(context.Context) (target.TargetInfo, error) { return f.info, nil }

func newTestPlugin(name string, client pluginClient) *Plugin {
	return &Plugin{
		name: name,
		path: "/tmp/plugin",
		ops:  &fakeOps{},
		newClient: func(context.Context, string, sdk.TargetInfo, sdk.HandleServer) (pluginClient, error) {
			return client, nil
		},
	}
}

func TestPluginReusesClientAcrossCalls(t *testing.T) {
	var (
		created int
		client  = &fakePluginClient{needsChange: true}
	)

	mod := &Plugin{
		name: "custom",
		path: "/tmp/plugin",
		ops:  &fakeOps{},
		newClient: func(context.Context, string, sdk.TargetInfo, sdk.HandleServer) (pluginClient, error) {
			created++
			return client, nil
		},
	}

	res, err := mod.Check(context.Background(), map[string]any{"name": "first"}, nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("Check() = false, want true")
	}

	if _, err := mod.Apply(context.Background(), map[string]any{"name": "first"}, nil); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	res, err = mod.Check(context.Background(), map[string]any{"name": "second"}, nil)
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("second Check() = false, want true")
	}

	if created != 1 {
		t.Fatalf("created %d clients, want 1", created)
	}
	if client.checkCalls != 2 {
		t.Fatalf("Check called %d times, want 2", client.checkCalls)
	}
	if client.applyCalls != 1 {
		t.Fatalf("Apply called %d times, want 1", client.applyCalls)
	}
	if client.closeCalls != 0 {
		t.Fatalf("Close called %d times before shutdown, want 0", client.closeCalls)
	}

	if err := mod.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("Close called %d times after shutdown, want 1", client.closeCalls)
	}
	if err := mod.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("Close called %d times after second shutdown, want 1", client.closeCalls)
	}
}

func TestPluginRejectsNameMismatch(t *testing.T) {
	client := &fakePluginClient{name: "wrong"}
	mod := newTestPlugin("custom", client)

	if _, err := mod.Check(context.Background(), nil, nil); err == nil {
		t.Fatal("Check() error = nil, want mismatch error")
	}
	if client.closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", client.closeCalls)
	}
}

func TestPluginCloseDropsCachedClient(t *testing.T) {
	clients := []*fakePluginClient{
		{needsChange: true},
		{needsChange: false},
	}
	created := 0

	mod := &Plugin{
		name: "custom",
		path: "/tmp/plugin",
		ops:  &fakeOps{},
		newClient: func(context.Context, string, sdk.TargetInfo, sdk.HandleServer) (pluginClient, error) {
			client := clients[created]
			created++
			return client, nil
		},
	}

	res, err := mod.Check(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("first Check() error = %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("first Check() = false, want true")
	}

	if err := mod.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	res, err = mod.Check(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if res.NeedsChange {
		t.Fatal("second Check() = true, want false")
	}

	if created != 2 {
		t.Fatalf("created %d clients, want 2", created)
	}
	if clients[0].closeCalls != 1 {
		t.Fatalf("first client Close called %d times, want 1", clients[0].closeCalls)
	}
	if clients[1].closeCalls != 0 {
		t.Fatalf("second client Close called %d times before shutdown, want 0", clients[1].closeCalls)
	}
}

func TestPluginForwardsMessageAndStreaming(t *testing.T) {
	client := &fakePluginClient{
		needsChange: true,
		checkMsg:    "needs update",
		applyMsg:    "applied ok",
		outputLines: []string{"line-a", "line-b"},
	}
	mod := newTestPlugin("custom", client)

	var checkOut []string
	checkRes, err := mod.Check(context.Background(), nil, func(line string) { checkOut = append(checkOut, line) })
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if checkRes.Message != "needs update" {
		t.Errorf("Check message = %q, want %q", checkRes.Message, "needs update")
	}
	if len(checkOut) != 2 {
		t.Errorf("Check output lines = %v, want 2", checkOut)
	}

	var applyOut []string
	applyRes, err := mod.Apply(context.Background(), nil, func(line string) { applyOut = append(applyOut, line) })
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if applyRes.Message != "applied ok" {
		t.Errorf("Apply message = %q, want %q", applyRes.Message, "applied ok")
	}
	if len(applyOut) != 2 {
		t.Errorf("Apply output lines = %v, want 2", applyOut)
	}
}

func TestPluginBindTarget(t *testing.T) {
	unbound := NewModule("custom", "/tmp/plugin").(*Plugin)
	if unbound.ops != nil {
		t.Fatal("unbound plugin should have nil ops")
	}
	ops := &fakeOps{}
	bound := unbound.BindTarget(ops).(*Plugin)
	if bound.ops != ops {
		t.Fatal("BindTarget did not bind ops")
	}
	// original stays unbound
	if unbound.ops != nil {
		t.Fatal("BindTarget mutated the unbound receiver")
	}
}
