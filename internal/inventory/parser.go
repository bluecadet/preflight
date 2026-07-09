package inventory

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// rawInventory is the intermediate YAML structure.
type rawInventory struct {
	Vars   map[string]any      `yaml:"vars"`
	Groups map[string]rawGroup `yaml:"groups"`
	Hosts  []rawHost           `yaml:"hosts"`
}

type rawGroup struct {
	Vars map[string]any `yaml:"vars"`
}

type rawHost struct {
	Name                 string         `yaml:"name"`
	Address              string         `yaml:"address"`
	Transport            string         `yaml:"transport"`
	Port                 int            `yaml:"port"`
	Username             string         `yaml:"username"`
	Password             string         `yaml:"password"`
	PrivateKey           string         `yaml:"private_key"`
	PrivateKeyPassphrase string         `yaml:"private_key_passphrase"`
	KnownHostsFile       string         `yaml:"known_hosts_file"`
	HostKeyAlgorithms    []string       `yaml:"host_key_algorithms"`
	HTTPS                bool           `yaml:"https"`
	Groups               []string       `yaml:"groups"`
	Vars                 map[string]any `yaml:"vars"`
	Timeout              string         `yaml:"timeout"`
}

// Parse parses inventory YAML data into an Inventory.
func Parse(data []byte) (*Inventory, error) {
	if err := ValidateYAML(data); err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("inventory: parse error: %w", err)
	}

	if len(root.Content) == 0 {
		return emptyInventory(), nil
	}

	return ParseNode(root.Content[0])
}

// ParseNode parses an already validated inventory YAML node. It is used by the
// project config parser for nested inventory blocks after config-level schema
// validation has succeeded.
func ParseNode(node *yaml.Node) (*Inventory, error) {
	if node == nil {
		return emptyInventory(), nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return emptyInventory(), nil
		}
		node = node.Content[0]
	}

	var raw rawInventory
	if err := node.Decode(&raw); err != nil {
		return nil, fmt.Errorf("inventory: parse error: %w", err)
	}

	inv := &Inventory{
		Vars:   raw.Vars,
		Groups: make(map[string]Group, len(raw.Groups)),
		Hosts:  make([]Host, 0, len(raw.Hosts)),
	}
	if inv.Vars == nil {
		inv.Vars = make(map[string]any)
	}

	for name, rg := range raw.Groups {
		inv.Groups[name] = Group{
			Name: name,
			Vars: rg.Vars,
		}
	}

	seenHosts := make(map[string]struct{}, len(raw.Hosts))
	for _, rh := range raw.Hosts {
		if rh.Name == "" {
			return nil, fmt.Errorf("inventory: host is missing a name")
		}
		if _, ok := seenHosts[rh.Name]; ok {
			return nil, fmt.Errorf("inventory: duplicate host name %q", rh.Name)
		}
		seenHosts[rh.Name] = struct{}{}

		for _, groupName := range rh.Groups {
			if _, ok := inv.Groups[groupName]; !ok {
				return nil, fmt.Errorf("inventory: host %q references undefined group %q", rh.Name, groupName)
			}
		}

		var timeout time.Duration
		if rh.Timeout != "" {
			parsed, err := time.ParseDuration(rh.Timeout)
			if err != nil {
				return nil, fmt.Errorf("inventory: host %q: invalid timeout %q: %w", rh.Name, rh.Timeout, err)
			}
			timeout = parsed
		}

		inv.Hosts = append(inv.Hosts, Host{
			Name:                 rh.Name,
			Address:              rh.Address,
			Transport:            defaultTransport(rh.Transport),
			Port:                 defaultPort(rh.Transport, rh.HTTPS, rh.Port),
			Username:             rh.Username,
			Password:             rh.Password,
			PrivateKey:           rh.PrivateKey,
			PrivateKeyPassphrase: rh.PrivateKeyPassphrase,
			KnownHostsFile:       rh.KnownHostsFile,
			HostKeyAlgorithms:    rh.HostKeyAlgorithms,
			HTTPS:                rh.HTTPS,
			Groups:               rh.Groups,
			Vars:                 rh.Vars,
			Timeout:              timeout,
		})
	}

	return inv, nil
}

func emptyInventory() *Inventory {
	return &Inventory{
		Vars:   make(map[string]any),
		Groups: make(map[string]Group),
		Hosts:  nil,
	}
}

func defaultTransport(t string) Transport {
	switch Transport(t) {
	case TransportWinRM, TransportSSH, TransportLocal:
		return Transport(t)
	default:
		return TransportWinRM
	}
}

func defaultPort(transport string, https bool, port int) int {
	if port != 0 {
		return port
	}
	switch Transport(transport) {
	case TransportSSH:
		return 22
	case TransportWinRM:
		if https {
			return 5986
		}
		return 5985
	default:
		return 0
	}
}
