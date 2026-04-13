package module

// EnvironmentParams are the typed parameters for the environment module.
type EnvironmentParams struct {
	Name   string `param:"name,required"`
	Value  string `param:"value"`
	Scope  string `param:"scope"`
	Ensure string `param:"ensure" default:"present"`
}

// ServiceParams are the typed parameters for the service module.
type ServiceParams struct {
	Name        string `param:"name,required"`
	State       string `param:"state"`
	StartupType string `param:"startup_type"`
}

// WindowsFeatureParams are the typed parameters for the windows_feature module.
type WindowsFeatureParams struct {
	Name   string `param:"name,required"`
	Ensure string `param:"ensure" default:"present"`
}

// ShortcutParams are the typed parameters for the shortcut module.
type ShortcutParams struct {
	Destination string `param:"destination,required"`
	Ensure      string `param:"ensure" default:"present"`
	Target      string `param:"target"`
	Args        string `param:"args"`
	Icon        string `param:"icon"`
}

// UserParams are the typed parameters for the user module.
type UserParams struct {
	Name     string `param:"name,required"`
	Ensure   string `param:"ensure" default:"present"`
	Password string `param:"password"`
}

// FirewallRuleParams are the typed parameters for the firewall_rule module.
type FirewallRuleParams struct {
	Name      string `param:"name,required"`
	Ensure    string `param:"ensure" default:"present"`
	Direction string `param:"direction"`
	Action    string `param:"action"`
	Protocol  string `param:"protocol"`
}

// RegistryParams are the typed parameters for the registry module (Go-side validation only).
type RegistryParams struct {
	Path   string `param:"path,required"`
	Ensure string `param:"ensure" default:"present"`
}

// PowerPlanParams are the typed parameters for the power_plan module.
type PowerPlanParams struct {
	Name   string `param:"name,required"`
	Ensure string `param:"ensure" default:"present"`
}

// ScheduledTaskParams are the typed parameters for the scheduled_task module (Go-side validation only).
type ScheduledTaskParams struct {
	Name string `param:"name,required"`
}
