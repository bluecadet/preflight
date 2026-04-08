package inventory

import (
	"fmt"
	"maps"
	"slices"
)

// Transport is the connection protocol to use for a target host.
type Transport string

const (
	TransportWinRM Transport = "winrm"
	TransportSSH   Transport = "ssh"
	TransportLocal Transport = "local"
)

// Host represents a single target machine.
type Host struct {
	Name              string
	Address           string
	Transport         Transport
	Port              int
	Username          string
	Password          string
	PrivateKey        string
	KnownHostsFile    string
	HostKeyAlgorithms []string
	HTTPS             bool
	Vars              map[string]any
}

// Group is a named set of hosts sharing common variables.
type Group struct {
	Name  string
	Vars  map[string]any
	Hosts []Host
}

// Inventory holds all groups and their hosts.
type Inventory struct {
	Groups     map[string]Group
	GroupOrder []string
}

// HostsForTarget returns the hosts for the given target, which may be a group
// name, a host name, or "all". Group vars (and "all" group vars) are merged
// into each host's Vars map (host vars take precedence).
func (inv *Inventory) HostsForTarget(target string) ([]Host, error) {
	if target == "all" {
		return inv.AllHosts(), nil
	}

	// Check if it's a group name.
	if g, ok := inv.Groups[target]; ok {
		result := make([]Host, len(g.Hosts))
		for i, h := range g.Hosts {
			result[i] = inv.mergedHost(h, g.Vars)
		}
		return result, nil
	}

	// Check if it's a host name.
	for _, groupName := range inv.orderedGroups() {
		g := inv.Groups[groupName]
		if g.Name == "all" {
			continue
		}
		for _, h := range g.Hosts {
			if h.Name == target {
				return []Host{inv.mergedHost(h, g.Vars)}, nil
			}
		}
	}

	return nil, fmt.Errorf("inventory: target %q not found (no group or host with that name)", target)
}

// AllHosts returns every host across all groups (deduplicated by name).
// Group vars are merged into each host's Vars.
func (inv *Inventory) AllHosts() []Host {
	seen := map[string]bool{}
	var hosts []Host

	for _, name := range inv.orderedGroups() {
		g := inv.Groups[name]
		if name == "all" {
			continue
		}
		for _, h := range g.Hosts {
			if seen[h.Name] {
				continue
			}
			seen[h.Name] = true
			hosts = append(hosts, inv.mergedHost(h, g.Vars))
		}
	}
	return hosts
}

// SelectTargets resolves a list of target selectors into a deduplicated list of
// hosts. Selectors are processed in order and the first occurrence of a host
// wins.
func (inv *Inventory) SelectTargets(selectors []string) ([]Host, error) {
	if len(selectors) == 0 {
		return nil, nil
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

// mergedHost returns a copy of h with allVars, groupVars, and host vars merged
// (later wins). The "all" group vars are applied first, then groupVars, then
// the host's own Vars.
func (inv *Inventory) mergedHost(h Host, groupVars map[string]any) Host {
	merged := make(map[string]any)

	// Apply "all" group vars first.
	if all, ok := inv.Groups["all"]; ok {
		maps.Copy(merged, all.Vars)
	}

	// Apply group vars.
	maps.Copy(merged, groupVars)

	// Apply host-level vars last (highest precedence).
	maps.Copy(merged, h.Vars)

	copy := h
	copy.Vars = merged
	return copy
}

func (inv *Inventory) orderedGroups() []string {
	if len(inv.GroupOrder) > 0 {
		return inv.GroupOrder
	}

	names := make([]string, 0, len(inv.Groups))
	for name := range inv.Groups {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
