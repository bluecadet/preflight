package target

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type fakePluginClient struct {
	checkCalls  int
	applyCalls  int
	closeCalls  int
	needsChange bool
	name        string
}

func (c *fakePluginClient) Check(_ map[string]any) (sdk.CheckResult, error) {
	c.checkCalls++
	return sdk.CheckResult{NeedsChange: c.needsChange}, nil
}

func (c *fakePluginClient) Apply(_ map[string]any) (sdk.ApplyResult, error) {
	c.applyCalls++
	return sdk.ApplyResult{}, nil
}

func (c *fakePluginClient) Close() error {
	c.closeCalls++
	return nil
}

func (c *fakePluginClient) Name() string { return c.name }

func TestPluginModuleReusesClientAcrossCalls(t *testing.T) {
	var (
		created int
		client  = &fakePluginClient{needsChange: true}
	)

	mod := &pluginModule{
		name: "custom",
		path: "/tmp/plugin",
		newClient: func(context.Context, string) (pluginClient, error) {
			created++
			return client, nil
		},
	}

	needed, err := mod.Check(context.Background(), map[string]any{"name": "first"})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !needed {
		t.Fatal("Check() = false, want true")
	}

	if err := mod.Apply(context.Background(), map[string]any{"name": "first"}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	needed, err = mod.Check(context.Background(), map[string]any{"name": "second"})
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if !needed {
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

func TestPluginModuleRejectsNameMismatch(t *testing.T) {
	client := &fakePluginClient{name: "wrong"}
	mod := &pluginModule{
		name: "custom",
		path: "/tmp/plugin",
		newClient: func(context.Context, string) (pluginClient, error) {
			return client, nil
		},
	}

	if _, err := mod.Check(context.Background(), nil); err == nil {
		t.Fatal("Check() error = nil, want mismatch error")
	}
	if client.closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", client.closeCalls)
	}
}

func TestPluginModuleCloseDropsCachedClient(t *testing.T) {
	clients := []*fakePluginClient{
		{needsChange: true},
		{needsChange: false},
	}
	created := 0

	mod := &pluginModule{
		name: "custom",
		path: "/tmp/plugin",
		newClient: func(context.Context, string) (pluginClient, error) {
			client := clients[created]
			created++
			return client, nil
		},
	}

	needed, err := mod.Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("first Check() error = %v", err)
	}
	if !needed {
		t.Fatal("first Check() = false, want true")
	}

	if err := mod.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	needed, err = mod.Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if needed {
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

func TestLocalTargetCloseClosesModuleResources(t *testing.T) {
	client := &fakePluginClient{needsChange: true}
	mod := &pluginModule{
		name: "custom",
		path: "/tmp/plugin",
		newClient: func(context.Context, string) (pluginClient, error) {
			return client, nil
		},
	}
	tgt := NewLocalTarget(ModuleRegistry{"custom": mod})

	if _, err := tgt.Execute(context.Background(), "task-1", "custom", nil, ExecutionOptions{}, false, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.closeCalls != 0 {
		t.Fatalf("Close called %d times before target shutdown, want 0", client.closeCalls)
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("target Close() error = %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("Close called %d times after target shutdown, want 1", client.closeCalls)
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("second target Close() error = %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("Close called %d times after second target shutdown, want 1", client.closeCalls)
	}
}
