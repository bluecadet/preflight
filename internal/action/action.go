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

// Output describes a named output emitted by an action.
type Output struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

// inlineModuleFields maps the YAML key of each inline module field to a
// function that extracts the params map from the Task.
var inlineModuleFields = []struct {
	name   string
	getter func(*Task) map[string]any
}{
	{"registry", func(t *Task) map[string]any { return t.Registry }},
	{"service", func(t *Task) map[string]any { return t.Service }},
	{"file", func(t *Task) map[string]any { return t.File }},
	{"directory", func(t *Task) map[string]any { return t.Directory }},
	{"package", func(t *Task) map[string]any { return t.Package }},
	{"shortcut", func(t *Task) map[string]any { return t.Shortcut }},
	{"scheduled_task", func(t *Task) map[string]any { return t.ScheduledTask }},
	{"user", func(t *Task) map[string]any { return t.User }},
	{"windows_feature", func(t *Task) map[string]any { return t.WindowsFeature }},
	{"environment", func(t *Task) map[string]any { return t.Environment }},
	{"firewall_rule", func(t *Task) map[string]any { return t.FirewallRule }},
	{"powershell", func(t *Task) map[string]any { return t.Powershell }},
	{"shell", func(t *Task) map[string]any { return t.Shell }},
	{"reboot", func(t *Task) map[string]any { return t.Reboot }},
	{"wait", func(t *Task) map[string]any { return t.Wait }},
}

// Task is a single step inside an action or playbook.
type Task struct {
	Name         string         `yaml:"name"`
	Uses         string         `yaml:"uses"`
	With         map[string]any `yaml:"with"`
	Module       string         `yaml:"-"` // resolved module name
	Params       map[string]any `yaml:"-"` // resolved module params
	When         string         `yaml:"when"`
	DependsOn    []string       `yaml:"depends_on"`
	IgnoreErrors bool           `yaml:"ignore_errors"`
	Tags         []string       `yaml:"tags"`

	// Inline module fields — at most one may be non-nil per task.
	Registry       map[string]any `yaml:"registry"`
	Service        map[string]any `yaml:"service"`
	File           map[string]any `yaml:"file"`
	Directory      map[string]any `yaml:"directory"`
	Package        map[string]any `yaml:"package"`
	Shortcut       map[string]any `yaml:"shortcut"`
	ScheduledTask  map[string]any `yaml:"scheduled_task"`
	User           map[string]any `yaml:"user"`
	WindowsFeature map[string]any `yaml:"windows_feature"`
	Environment    map[string]any `yaml:"environment"`
	FirewallRule   map[string]any `yaml:"firewall_rule"`
	Powershell     map[string]any `yaml:"powershell"`
	Shell          map[string]any `yaml:"shell"`
	Reboot         map[string]any `yaml:"reboot"`
	Wait           map[string]any `yaml:"wait"`
}

// ResolveModule inspects inline module fields and sets Module + Params.
// Returns an error if more than one inline module field is set, or if both
// "uses" and an inline module field are set.
func (t *Task) ResolveModule() error {
	var found []string
	for _, f := range inlineModuleFields {
		if f.getter(t) != nil {
			found = append(found, f.name)
		}
	}

	if len(found) > 1 {
		return fmt.Errorf("task %q: multiple inline module fields set: %v (only one is allowed)", t.Name, found)
	}

	if len(found) == 1 {
		if t.Uses != "" {
			return fmt.Errorf("task %q: cannot set both 'uses' and inline module field %q", t.Name, found[0])
		}
		t.Module = found[0]
		for _, f := range inlineModuleFields {
			if f.name == found[0] {
				t.Params = f.getter(t)
				break
			}
		}
	}

	return nil
}

// Action is the parsed representation of an action.yml file.
type Action struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Author      string            `yaml:"author"`
	Inputs      map[string]Input  `yaml:"inputs"`
	Outputs     map[string]Output `yaml:"outputs"`
	Tasks       []Task            `yaml:"tasks"`
}

// ParseAction parses action YAML bytes into an Action.
func ParseAction(data []byte) (*Action, error) {
	var a Action
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("action: parse error: %w", err)
	}
	return &a, nil
}

// Playbook is the parsed representation of a playbook.yml file.
type Playbook struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Vars        map[string]any `yaml:"vars"`
	Import      []string       `yaml:"import"`
	Tasks       []Task         `yaml:"tasks"`
}

// ParsePlaybook parses playbook YAML bytes into a Playbook.
func ParsePlaybook(data []byte) (*Playbook, error) {
	var p Playbook
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("playbook: parse error: %w", err)
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
