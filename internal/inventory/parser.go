package inventory

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bluecadet/preflight/internal/target"
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
	Name                 string           `yaml:"name"`
	Address              string           `yaml:"address"`
	Transport            string           `yaml:"transport"`
	Platform             *target.Platform `yaml:"platform"`
	Port                 int              `yaml:"port"`
	Username             string           `yaml:"username"`
	Password             string           `yaml:"password"`
	PrivateKey           string           `yaml:"private_key"`
	PrivateKeyPassphrase string           `yaml:"private_key_passphrase"`
	KnownHostsFile       string           `yaml:"known_hosts_file"`
	HostKeyPolicy        string           `yaml:"host_key_policy"`
	HostKeyAlgorithms    []string         `yaml:"host_key_algorithms"`
	HTTPS                bool             `yaml:"https"`
	Groups               []string         `yaml:"groups"`
	Vars                 map[string]any   `yaml:"vars"`
	Timeout              string           `yaml:"timeout"`
	Jump                 *JumpHost        `yaml:"jump"`
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

		h, err := hostFromRaw(rh, inv.Groups)
		if err != nil {
			return nil, err
		}
		inv.Hosts = append(inv.Hosts, h)
	}

	return inv, nil
}

// hostFromRaw validates a single decoded host entry against groups (its
// group references, timeout, and host_key_policy/jump settings) and maps it
// into a Host. The caller is responsible for the host-name presence and
// duplicate checks, since those require state shared across all hosts.
func hostFromRaw(rh rawHost, groups map[string]Group) (Host, error) {
	for _, groupName := range rh.Groups {
		if _, ok := groups[groupName]; !ok {
			return Host{}, fmt.Errorf("inventory: host %q references undefined group %q", rh.Name, groupName)
		}
	}

	var timeout time.Duration
	if rh.Timeout != "" {
		parsed, err := time.ParseDuration(rh.Timeout)
		if err != nil {
			return Host{}, fmt.Errorf("inventory: host %q: invalid timeout %q: %w", rh.Name, rh.Timeout, err)
		}
		if parsed <= 0 {
			return Host{}, fmt.Errorf("inventory: host %q: timeout must be positive, got %q", rh.Name, rh.Timeout)
		}
		timeout = parsed
	}

	if err := validateHostKeyPolicy(rh.Name, "host_key_policy", rh.HostKeyPolicy); err != nil {
		return Host{}, err
	}

	if rh.Jump != nil {
		if rh.Jump.Address == "" {
			return Host{}, fmt.Errorf("inventory: host %q: jump.address is required", rh.Name)
		}
		if err := validateHostKeyPolicy(rh.Name, "jump host_key_policy", rh.Jump.HostKeyPolicy); err != nil {
			return Host{}, err
		}
	}

	return Host{
		Name:                 rh.Name,
		Address:              rh.Address,
		Transport:            defaultTransport(rh.Transport),
		Platform:             rh.Platform,
		Port:                 defaultPort(rh.Transport, rh.HTTPS, rh.Port),
		Username:             rh.Username,
		Password:             rh.Password,
		PrivateKey:           rh.PrivateKey,
		PrivateKeyPassphrase: rh.PrivateKeyPassphrase,
		KnownHostsFile:       rh.KnownHostsFile,
		HostKeyPolicy:        rh.HostKeyPolicy,
		HostKeyAlgorithms:    rh.HostKeyAlgorithms,
		HTTPS:                rh.HTTPS,
		Groups:               rh.Groups,
		Vars:                 rh.Vars,
		Timeout:              timeout,
		Jump:                 rh.Jump,
	}, nil
}

// validateHostKeyPolicy checks value against the valid SSH host-key policy
// values (empty, meaning the default, or one of the target package's
// exported HostKeyPolicy constants), returning a descriptive error for
// hostName/field otherwise. It is shared by the host-level and jump-level
// host_key_policy checks.
func validateHostKeyPolicy(hostName, field, value string) error {
	switch value {
	case "", target.HostKeyPolicyAcceptNew, target.HostKeyPolicyStrict, target.HostKeyPolicyInsecure:
		return nil
	default:
		return fmt.Errorf("inventory: host %q: invalid %s %q: must be %q, %q, or %q", hostName, field, value, target.HostKeyPolicyAcceptNew, target.HostKeyPolicyStrict, target.HostKeyPolicyInsecure)
	}
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
		return TransportSSH
	}
}

func defaultPort(transport string, https bool, port int) int {
	if port != 0 {
		return port
	}
	switch Transport(transport) {
	case TransportSSH, "":
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
