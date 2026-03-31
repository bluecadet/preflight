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
	Vars  map[string]any `yaml:"vars"`
	Hosts []rawHost      `yaml:"hosts"`
}

type rawHost struct {
	Name           string         `yaml:"name"`
	Address        string         `yaml:"address"`
	Transport      string         `yaml:"transport"`
	Port           int            `yaml:"port"`
	Username       string         `yaml:"username"`
	Password       string         `yaml:"password"`
	PasswordFrom   string         `yaml:"password_from"`
	PrivateKey     string         `yaml:"private_key"`
	PrivateKeyFrom string         `yaml:"private_key_from"`
	HTTPS          bool           `yaml:"https"`
	Vars           map[string]any `yaml:"vars"`
}

// Parse parses inventory YAML data into an Inventory.
func Parse(data []byte) (*Inventory, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("inventory: parse error: %w", err)
	}

	var raw rawInventory
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("inventory: parse error: %w", err)
	}

	inv := &Inventory{
		Groups:     make(map[string]Group, len(raw.Groups)),
		GroupOrder: inventoryGroupOrder(&root),
	}

	for name, rg := range raw.Groups {
		hosts := make([]Host, 0, len(rg.Hosts))
		for _, rh := range rg.Hosts {
			if rh.Name == "" {
				return nil, fmt.Errorf("inventory: host in group %q is missing a name", name)
			}
			h := Host{
				Name:           rh.Name,
				Address:        rh.Address,
				Transport:      defaultTransport(rh.Transport),
				Port:           defaultPort(rh.Transport, rh.Port),
				Username:       rh.Username,
				Password:       rh.Password,
				PasswordFrom:   rh.PasswordFrom,
				PrivateKey:     rh.PrivateKey,
				PrivateKeyFrom: rh.PrivateKeyFrom,
				HTTPS:          rh.HTTPS,
				Vars:           rh.Vars,
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

func inventoryGroupOrder(root *yaml.Node) []string {
	if root == nil || len(root.Content) == 0 {
		return nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i]
		value := doc.Content[i+1]
		if key.Value != "groups" || value.Kind != yaml.MappingNode {
			continue
		}

		order := make([]string, 0, len(value.Content)/2)
		for j := 0; j+1 < len(value.Content); j += 2 {
			order = append(order, value.Content[j].Value)
		}
		return order
	}

	return nil
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
