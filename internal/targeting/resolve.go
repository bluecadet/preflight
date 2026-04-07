package targeting

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

// ResolvedHost is a fully prepared execution target, including merged
// inventory vars, safe template metadata, derived state path, and a concrete
// target implementation.
type ResolvedHost struct {
	Name         string
	Vars         map[string]any
	TargetVars   map[string]any
	StatePath    string
	Target       target.Target
	InventoryRef inventory.Host
}

func ResolveHosts(
	ctx context.Context,
	inv *inventory.Inventory,
	selectors []string,
	registry target.ModuleRegistry,
	resolver *secrets.Resolver,
	baseStatePath string,
) ([]ResolvedHost, error) {
	if inv == nil {
		return nil, fmt.Errorf("resolve hosts: nil inventory")
	}

	hosts, err := inv.SelectTargets(selectors)
	if err != nil {
		return nil, err
	}

	resolved := make([]ResolvedHost, 0, len(hosts))
	for _, host := range hosts {
		prepared, err := prepareHost(ctx, host, registry, resolver, hostStatePath(baseStatePath, host.Name))
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, prepared)
	}

	return resolved, nil
}

func ResolveLocalHost(registry target.ModuleRegistry, statePath string) ResolvedHost {
	return ResolvedHost{
		Name: "localhost",
		Vars: map[string]any{},
		TargetVars: map[string]any{
			"name":      "localhost",
			"hostname":  "localhost",
			"address":   "localhost",
			"transport": string(inventory.TransportLocal),
		},
		StatePath: statePath,
		Target:    target.NewLocalTarget(registry),
		InventoryRef: inventory.Host{
			Name:      "localhost",
			Address:   "localhost",
			Transport: inventory.TransportLocal,
		},
	}
}

func prepareHost(
	ctx context.Context,
	host inventory.Host,
	registry target.ModuleRegistry,
	resolver *secrets.Resolver,
	statePath string,
) (ResolvedHost, error) {
	auth := map[string]any{
		"password":         host.Password,
		"password_from":    host.PasswordFrom,
		"private_key":      host.PrivateKey,
		"private_key_from": host.PrivateKeyFrom,
	}
	if resolver != nil && resolver.HasProviders() {
		resolved, err := resolver.ResolveMap(ctx, auth)
		if err != nil {
			return ResolvedHost{}, fmt.Errorf("resolve host %q auth: %w", host.Name, err)
		}
		auth = resolved
	}

	tgt, err := buildTarget(host, auth, registry)
	if err != nil {
		return ResolvedHost{}, err
	}

	return ResolvedHost{
		Name:         host.Name,
		Vars:         cloneMap(host.Vars),
		TargetVars:   safeTargetVars(host),
		StatePath:    statePath,
		Target:       tgt,
		InventoryRef: host,
	}, nil
}

func buildTarget(host inventory.Host, auth map[string]any, registry target.ModuleRegistry) (target.Target, error) {
	address := host.Address
	if address == "" {
		address = host.Name
	}

	switch host.Transport {
	case inventory.TransportLocal:
		return target.NewLocalTarget(registry), nil
	case inventory.TransportWinRM:
		password, _ := auth["password"].(string)
		return target.NewWinRMTarget(target.WinRMConfig{
			Host:     address,
			Port:     host.Port,
			Username: host.Username,
			Password: password,
			HTTPS:    host.HTTPS,
		}), nil
	case inventory.TransportSSH:
		password, _ := auth["password"].(string)
		privateKey, _ := auth["private_key"].(string)
		return target.NewSSHTarget(target.SSHConfig{
			Host:       address,
			Port:       host.Port,
			Username:   host.Username,
			Password:   password,
			PrivateKey: privateKey,
		}, registry), nil
	default:
		return nil, fmt.Errorf("resolve host %q: unsupported transport %q", host.Name, host.Transport)
	}
}

func safeTargetVars(host inventory.Host) map[string]any {
	address := host.Address
	if address == "" {
		address = host.Name
	}

	return map[string]any{
		"name":      host.Name,
		"hostname":  host.Name,
		"address":   address,
		"transport": string(host.Transport),
		"port":      host.Port,
	}
}

func hostStatePath(baseStatePath, hostname string) string {
	if baseStatePath == "" {
		return filepath.Join("state", "targets", hostname+".json")
	}
	return filepath.Join(filepath.Dir(baseStatePath), "targets", hostname+".json")
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	maps.Copy(dst, src)
	return dst
}
