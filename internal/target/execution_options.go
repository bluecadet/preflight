package target

import "fmt"

// ExecutionOptions are task-level execution settings applied outside module params.
type ExecutionOptions struct {
	Become *BecomeOptions `json:"become,omitempty"`
}

// BecomeOptions describe task execution under another user identity.
type BecomeOptions struct {
	Enabled     bool   `json:"enabled,omitempty"`
	User        string `json:"user,omitempty"`
	Password    string `json:"password,omitempty"`
	Method      string `json:"method,omitempty"`
	LoadProfile *bool  `json:"load_profile,omitempty"`
}

// NormalizeExecutionOptions validates a rendered execution-options map.
func NormalizeExecutionOptions(raw map[string]any) (ExecutionOptions, error) {
	if len(raw) == 0 {
		return ExecutionOptions{}, nil
	}

	becomeRaw, ok := raw["become"]
	if !ok || becomeRaw == nil {
		return ExecutionOptions{}, nil
	}
	becomeMap, ok := becomeRaw.(map[string]any)
	if !ok {
		return ExecutionOptions{}, fmt.Errorf("become: expected object, got %T", becomeRaw)
	}
	if len(becomeMap) == 0 {
		return ExecutionOptions{}, nil
	}

	enabled := true
	if value, ok := becomeMap["enabled"]; ok && value != nil {
		flag, ok := value.(bool)
		if !ok {
			return ExecutionOptions{}, fmt.Errorf("become: enabled must be a bool, got %T", value)
		}
		enabled = flag
	}
	user, err := executionString(becomeMap, "user")
	if err != nil {
		return ExecutionOptions{}, err
	}
	password, err := executionString(becomeMap, "password")
	if err != nil {
		return ExecutionOptions{}, err
	}
	method, err := executionString(becomeMap, "method")
	if err != nil {
		return ExecutionOptions{}, err
	}

	var loadProfile *bool
	if value, ok := becomeMap["load_profile"]; ok && value != nil {
		flag, ok := value.(bool)
		if !ok {
			return ExecutionOptions{}, fmt.Errorf("become: load_profile must be a bool, got %T", value)
		}
		loadProfile = &flag
	}

	if enabled && user == "" {
		return ExecutionOptions{}, fmt.Errorf("become: user is required when enabled")
	}

	return ExecutionOptions{
		Become: &BecomeOptions{
			Enabled:     enabled,
			User:        user,
			Password:    password,
			Method:      method,
			LoadProfile: loadProfile,
		},
	}, nil
}

func executionString(values map[string]any, key string) (string, error) {
	value, ok := values[key]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("become: %s must be a string, got %T", key, value)
	}
	return text, nil
}

// Enabled reports whether task execution should switch identity.
func (o ExecutionOptions) Enabled() bool {
	return o.Become != nil && o.Become.Enabled
}

// ToMap converts the execution options back to a generic map for hashing and staging.
func (o ExecutionOptions) ToMap() map[string]any {
	if o.Become == nil {
		return nil
	}

	become := map[string]any{
		"enabled": o.Become.Enabled,
		"user":    o.Become.User,
	}
	if o.Become.Password != "" {
		become["password"] = o.Become.Password
	}
	if o.Become.Method != "" {
		become["method"] = o.Become.Method
	}
	if o.Become.LoadProfile != nil {
		become["load_profile"] = *o.Become.LoadProfile
	}
	return map[string]any{"become": become}
}
