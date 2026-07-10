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

// hostAuth is a typed credential set for a single host (or its jump host),
// resolved through the secrets resolver before it reaches target config.
type hostAuth struct {
	password   string
	privateKey string
	passphrase string
}

// resolveAuth resolves any secret references in a. When resolver is nil or
// has no configured providers, a is returned unchanged.
func resolveAuth(ctx context.Context, resolver *secrets.Resolver, a hostAuth) (hostAuth, error) {
	if resolver == nil || !resolver.HasProviders() {
		return a, nil
	}

	password, err := resolveSecretString(ctx, resolver, a.password)
	if err != nil {
		return hostAuth{}, err
	}
	privateKey, err := resolveSecretString(ctx, resolver, a.privateKey)
	if err != nil {
		return hostAuth{}, err
	}
	passphrase, err := resolveSecretString(ctx, resolver, a.passphrase)
	if err != nil {
		return hostAuth{}, err
	}

	return hostAuth{password: password, privateKey: privateKey, passphrase: passphrase}, nil
}

func resolveSecretString(ctx context.Context, resolver *secrets.Resolver, s string) (string, error) {
	if !secrets.IsRef(s) {
		return s, nil
	}
	return resolver.ResolveRef(ctx, s)
}

func prepareHost(
	ctx context.Context,
	host inventory.Host,
	registry target.ModuleRegistry,
	resolver *secrets.Resolver,
	statePath string,
) (ResolvedHost, error) {
	auth, err := resolveAuth(ctx, resolver, hostAuth{
		password:   host.Password,
		privateKey: host.PrivateKey,
		passphrase: host.PrivateKeyPassphrase,
	})
	if err != nil {
		return ResolvedHost{}, fmt.Errorf("resolve host %q auth: %w", host.Name, err)
	}

	var jumpAuth hostAuth
	if host.Jump != nil {
		jumpAuth, err = resolveAuth(ctx, resolver, hostAuth{
			password:   host.Jump.Password,
			privateKey: host.Jump.PrivateKey,
			passphrase: host.Jump.PrivateKeyPassphrase,
		})
		if err != nil {
			return ResolvedHost{}, fmt.Errorf("resolve host %q jump auth: %w", host.Name, err)
		}
	}

	tgt, err := buildTarget(host, auth, jumpAuth, registry)
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

func buildTarget(host inventory.Host, auth, jumpAuth hostAuth, registry target.ModuleRegistry) (target.Target, error) {
	address := host.Address
	if address == "" {
		address = host.Name
	}

	switch host.Transport {
	case inventory.TransportLocal:
		return target.NewLocalTarget(registry), nil
	case inventory.TransportWinRM:
		return target.NewWinRMTarget(target.WinRMConfig{
			Host:     address,
			Port:     host.Port,
			Username: host.Username,
			Password: auth.password,
			HTTPS:    host.HTTPS,
			Timeout:  host.Timeout,
		}, registry), nil
	case inventory.TransportSSH:
		cfg := target.SSHConfig{
			Host:                 address,
			Port:                 host.Port,
			Username:             host.Username,
			Password:             auth.password,
			PrivateKey:           auth.privateKey,
			PrivateKeyPassphrase: auth.passphrase,
			KnownHostsFile:       host.KnownHostsFile,
			HostKeyPolicy:        host.HostKeyPolicy,
			HostKeyAlgorithms:    host.HostKeyAlgorithms,
			Timeout:              host.Timeout,
		}
		if host.Jump != nil {
			cfg.Jump = &target.SSHConfig{
				Host:                 host.Jump.Address,
				Port:                 host.Jump.Port,
				Username:             host.Jump.Username,
				Password:             jumpAuth.password,
				PrivateKey:           jumpAuth.privateKey,
				PrivateKeyPassphrase: jumpAuth.passphrase,
				KnownHostsFile:       host.Jump.KnownHostsFile,
				HostKeyPolicy:        host.Jump.HostKeyPolicy,
			}
		}
		return target.NewSSHTarget(cfg, registry), nil
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
