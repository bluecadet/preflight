package inventory

import (
	"fmt"
	"slices"
	"time"

	"github.com/bluecadet/preflight/internal/maputil"
	"github.com/bluecadet/preflight/internal/target"
)

// Transport is the connection protocol to use for a target host.
type Transport = target.Transport

const (
	TransportWinRM = target.TransportWinRM
	TransportSSH   = target.TransportSSH
	TransportLocal = target.TransportLocal
)

// Host represents a single target machine.
type Host struct {
	Name                 string         `yaml:"name"`
	Address              string         `yaml:"address,omitempty"`
	Transport            Transport      `yaml:"transport,omitempty"`
	Port                 int            `yaml:"port,omitempty"`
	Username             string         `yaml:"username,omitempty"`
	Password             string         `yaml:"password,omitempty"`
	PrivateKey           string         `yaml:"private_key,omitempty"`
	PrivateKeyPassphrase string         `yaml:"private_key_passphrase,omitempty"`
	KnownHostsFile       string         `yaml:"known_hosts_file,omitempty"`
	HostKeyAlgorithms    []string       `yaml:"host_key_algorithms,omitempty"`
	HTTPS                bool           `yaml:"https,omitempty"`
	Groups               []string       `yaml:"groups,omitempty"`
	Vars                 map[string]any `yaml:"vars,omitempty"`
	// Timeout is the connection/handshake timeout for SSH and WinRM
	// transports. Zero means unset, which falls back to each transport's
	// own default (30s for SSH).
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

// Group is optional metadata for hosts that opt into the group by name.
type Group struct {
	Name string         `yaml:"-"`
	Vars map[string]any `yaml:"vars,omitempty"`
}

// Inventory holds host-first project inventory.
type Inventory struct {
	Vars   map[string]any   `yaml:"vars,omitempty"`
	Groups map[string]Group `yaml:"groups,omitempty"`
	Hosts  []Host           `yaml:"hosts,omitempty"`
}

// HostsForTarget returns the hosts for the given target, which may be "all", a
// group name, or a host name. Inventory vars, group vars in each host's group
// order, and host vars are merged into each returned host's Vars map.
func (inv *Inventory) HostsForTarget(selector string) ([]Host, error) {
	if selector == "" || selector == "all" {
		return inv.AllHosts(), nil
	}

	if _, ok := inv.Groups[selector]; ok {
		var result []Host
		for _, h := range inv.Hosts {
			if slices.Contains(h.Groups, selector) {
				result = append(result, inv.mergedHost(h))
			}
		}
		return result, nil
	}

	for _, h := range inv.Hosts {
		if h.Name == selector {
			return []Host{inv.mergedHost(h)}, nil
		}
	}

	return nil, fmt.Errorf("inventory: target %q not found (no group or host with that name)", selector)
}

// AllHosts returns every configured host with inventory, group, and host vars
// merged.
func (inv *Inventory) AllHosts() []Host {
	hosts := make([]Host, 0, len(inv.Hosts))
	for _, h := range inv.Hosts {
		hosts = append(hosts, inv.mergedHost(h))
	}
	return hosts
}

// SelectTargets resolves a list of target selectors into a deduplicated list of
// hosts. Selectors are processed in order and the first occurrence of a host
// wins.
func (inv *Inventory) SelectTargets(selectors []string) ([]Host, error) {
	if len(selectors) == 0 {
		return inv.AllHosts(), nil
	}

	seen := make(map[string]bool)
	var result []Host
	for _, selector := range selectors {
		hosts, err := inv.HostsForTarget(selector)
		if err != nil {
			return nil, err
		}
		for _, host := range hosts {
			if seen[host.Name] {
				continue
			}
			seen[host.Name] = true
			result = append(result, host)
		}
	}

	return result, nil
}

// mergedHost returns a copy of h with inventory vars, group vars in the order
// listed by the host, and host vars merged (later wins). Merges are deep so
// nested maps are merged key-by-key rather than replaced wholesale.
func (inv *Inventory) mergedHost(h Host) Host {
	merged := make(map[string]any)

	maputil.DeepMerge(merged, inv.Vars)
	for _, groupName := range h.Groups {
		if g, ok := inv.Groups[groupName]; ok {
			maputil.DeepMerge(merged, g.Vars)
		}
	}
	maputil.DeepMerge(merged, h.Vars)

	copy := h
	copy.Vars = merged
	return copy
}
