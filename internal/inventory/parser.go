package inventory

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// rawInventory is the intermediate YAML structure.
type rawInventory struct {
	Groups map[string]rawGroup `yaml:"groups"`
}

type rawGroup struct {
	Vars  map[string]interface{} `yaml:"vars"`
	Hosts []rawHost              `yaml:"hosts"`
}

type rawHost struct {
	Name       string                 `yaml:"name"`
	Address    string                 `yaml:"address"`
	Transport  string                 `yaml:"transport"`
	Port       int                    `yaml:"port"`
	Username   string                 `yaml:"username"`
	Password   string                 `yaml:"password"`
	PrivateKey string                 `yaml:"private_key"`
	HTTPS      bool                   `yaml:"https"`
	Vars       map[string]interface{} `yaml:"vars"`
}

// Parse parses inventory YAML data into an Inventory.
func Parse(data []byte) (*Inventory, error) {
	var raw rawInventory
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("inventory: parse error: %w", err)
	}

	inv := &Inventory{
		Groups: make(map[string]Group, len(raw.Groups)),
	}

	for name, rg := range raw.Groups {
		hosts := make([]Host, 0, len(rg.Hosts))
		for _, rh := range rg.Hosts {
			if rh.Name == "" {
				return nil, fmt.Errorf("inventory: host in group %q is missing a name", name)
			}
			h := Host{
				Name:       rh.Name,
				Address:    rh.Address,
				Transport:  defaultTransport(rh.Transport),
				Port:       defaultPort(rh.Transport, rh.Port),
				Username:   rh.Username,
				Password:   rh.Password,
				PrivateKey: rh.PrivateKey,
				HTTPS:      rh.HTTPS,
				Vars:       rh.Vars,
			}
			hosts = append(hosts, h)
		}

		inv.Groups[name] = Group{
			Name:  name,
			Vars:  rg.Vars,
			Hosts: hosts,
		}
	}

	return inv, nil
}

// ParseFile reads and parses an inventory YAML file.
func ParseFile(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("inventory: read file %q: %w", path, err)
	}
	return Parse(data)
}

func defaultTransport(t string) Transport {
	switch Transport(t) {
	case TransportWinRM, TransportSSH, TransportLocal:
		return Transport(t)
	default:
		return TransportWinRM
	}
}

func defaultPort(transport string, port int) int {
	if port != 0 {
		return port
	}
	switch Transport(transport) {
	case TransportSSH:
		return 22
	default:
		return 5985
	}
}
