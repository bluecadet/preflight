package action

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Input describes a typed input parameter for an action.
type Input struct {
	Type        string `yaml:"type"` // string, bool, int, path
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

// TaskDefaults describes execution defaults inherited by tasks.
type TaskDefaults struct {
	Become map[string]any `yaml:"become" json:"become,omitempty"`
}

var knownInlineModules = []string{
	"registry",
	"service",
	"file",
	"directory",
	"package",
	"shortcut",
	"scheduled_task",
	"user",
	"winget_package",
	"remove_appx_packages",
	"power_plan",
	"windows_feature",
	"environment",
	"firewall_rule",
	"powershell",
	"shell",
	"reboot",
	"wait",
}

var knownInlineModuleSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(knownInlineModules))
	for _, name := range knownInlineModules {
		set[name] = struct{}{}
	}
	return set
}()

// Task is a single step inside an action or playbook.
//
// Module and Params are the canonical internal representation used after
// parsing/normalization. InlineModules preserves decode-time sugar for known
// inline module YAML keys.
type Task struct {
	Name         string         `yaml:"name"`
	Uses         string         `yaml:"uses"`
	With         map[string]any `yaml:"with"`
	Become       map[string]any `yaml:"become" json:"become,omitempty"`
	ModuleName   string         `yaml:"module"`
	ModuleParams map[string]any `yaml:"params"`
	Module       string         `yaml:"-"` // canonical module name
	Params       map[string]any `yaml:"-"` // canonical module params
	When         string         `yaml:"when"`
	DependsOn    []string       `yaml:"depends_on"`
	IgnoreErrors bool           `yaml:"ignore_errors"`
	Tags         []string       `yaml:"tags"`

	// InlineModules stores known inline module definitions by YAML module name.
	InlineModules map[string]map[string]any `yaml:"-"`
}

type taskKnownFields struct {
	Name         string         `yaml:"name"`
	Uses         string         `yaml:"uses"`
	With         map[string]any `yaml:"with"`
	Become       map[string]any `yaml:"become" json:"become,omitempty"`
	ModuleName   string         `yaml:"module"`
	ModuleParams map[string]any `yaml:"params"`
	When         string         `yaml:"when"`
	DependsOn    []string       `yaml:"depends_on"`
	IgnoreErrors bool           `yaml:"ignore_errors"`
	Tags         []string       `yaml:"tags"`
}

func (t *Task) UnmarshalYAML(value *yaml.Node) error {
	var decoded taskKnownFields
	if err := value.Decode(&decoded); err != nil {
		return err
	}

	*t = Task{
		Name:         decoded.Name,
		Uses:         decoded.Uses,
		With:         decoded.With,
		Become:       decoded.Become,
		ModuleName:   decoded.ModuleName,
		ModuleParams: decoded.ModuleParams,
		When:         decoded.When,
		DependsOn:    decoded.DependsOn,
		IgnoreErrors: decoded.IgnoreErrors,
		Tags:         decoded.Tags,
	}

	if value.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i].Value
		if _, ok := knownInlineModuleSet[key]; !ok {
			continue
		}

		var params map[string]any
		if err := value.Content[i+1].Decode(&params); err != nil {
			return fmt.Errorf("task %q: decode inline module %q: %w", decoded.Name, key, err)
		}
		if params == nil {
			continue
		}
		if t.InlineModules == nil {
			t.InlineModules = make(map[string]map[string]any, 1)
		}
		t.InlineModules[key] = params
	}

	return nil
}

// ResolveModule canonicalizes a task into its internal module+params form.
// Returns an error if more than one inline module field is set, or if both
// "uses" and a concrete module are set.
func (t *Task) ResolveModule() error {
	module, params, found, err := resolveTaskModule(t)
	if err != nil {
		return err
	}
	if !found {
		t.Module = ""
		t.Params = nil
		return nil
	}
	t.Module = module
	t.Params = params
	return nil
}

// Action is the parsed representation of an action.yml file.
type Action struct {
	Name        string           `yaml:"name"`
	Version     string           `yaml:"version"`
	Description string           `yaml:"description"`
	Author      string           `yaml:"author"`
	Defaults    TaskDefaults     `yaml:"defaults" json:"defaults"`
	Inputs      map[string]Input `yaml:"inputs"`
	Tasks       []Task           `yaml:"tasks"`
}

// Normalize canonicalizes all tasks in the action.
func (a *Action) Normalize() error {
	if a == nil {
		return nil
	}
	for i := range a.Tasks {
		if err := a.Tasks[i].ResolveModule(); err != nil {
			return err
		}
	}
	return nil
}

// ParseAction parses action YAML bytes into an Action.
func ParseAction(data []byte) (*Action, error) {
	if err := ValidateActionYAML(data); err != nil {
		return nil, fmt.Errorf("action: %w", err)
	}
	var a Action
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("action: parse error: %w", err)
	}
	if err := a.Normalize(); err != nil {
		return nil, err
	}
	return &a, nil
}

// Playbook is the parsed representation of a playbook.yml file.
type Playbook struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Defaults    TaskDefaults   `yaml:"defaults" json:"defaults"`
	Vars        map[string]any `yaml:"vars"`
	Import      []string       `yaml:"import"`
	Tasks       []Task         `yaml:"tasks"`
}

// Normalize canonicalizes all tasks in the playbook.
func (p *Playbook) Normalize() error {
	if p == nil {
		return nil
	}
	for i := range p.Tasks {
		if err := p.Tasks[i].ResolveModule(); err != nil {
			return err
		}
	}
	return nil
}

// ParsePlaybook parses playbook YAML bytes into a Playbook.
func ParsePlaybook(data []byte) (*Playbook, error) {
	if err := ValidatePlaybookYAML(data); err != nil {
		return nil, fmt.Errorf("playbook: %w", err)
	}
	var p Playbook
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("playbook: parse error: %w", err)
	}
	if err := p.Normalize(); err != nil {
		return nil, err
	}
	return &p, nil
}

// ParsePlaybookFile reads a file at path and parses it as a Playbook.
func ParsePlaybookFile(path string) (*Playbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("playbook: read %q: %w", path, err)
	}
	return ParsePlaybook(data)
}

func resolveTaskModule(t *Task) (string, map[string]any, bool, error) {
	var found []string
	for _, name := range knownInlineModules {
		if t.InlineModules[name] != nil {
			found = append(found, name)
		}
	}

	if len(found) > 1 {
		return "", nil, false, fmt.Errorf("task %q: multiple inline module fields set: %v (only one is allowed)", t.Name, found)
	}

	if t.ModuleName != "" {
		if t.Uses != "" {
			return "", nil, false, fmt.Errorf("task %q: cannot set both 'uses' and 'module'", t.Name)
		}
		if len(found) > 0 {
			return "", nil, false, fmt.Errorf("task %q: cannot set both 'module' and inline module field %q", t.Name, found[0])
		}
		return t.ModuleName, t.ModuleParams, true, nil
	}

	if len(found) == 1 {
		if t.Uses != "" {
			return "", nil, false, fmt.Errorf("task %q: cannot set both 'uses' and inline module field %q", t.Name, found[0])
		}
		return found[0], t.InlineModules[found[0]], true, nil
	}

	if t.ModuleParams != nil {
		return "", nil, false, fmt.Errorf("task %q: cannot set 'params' without 'module'", t.Name)
	}

	return "", nil, false, nil
}
