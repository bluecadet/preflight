package preflighterr

import "fmt"

// ModuleError wraps an error from a specific module.
type ModuleError struct {
	Module string
	Op     string
	Err    error
}

func (e *ModuleError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("module %q: %s: %s", e.Module, e.Op, e.Err)
	}
	return fmt.Sprintf("module %q: %s", e.Module, e.Err)
}

func (e *ModuleError) Unwrap() error { return e.Err }

// TargetError wraps an error from a specific target transport.
type TargetError struct {
	Transport string // "local", "ssh", "winrm"
	Op        string
	Err       error
}

func (e *TargetError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("target/%s: %s: %s", e.Transport, e.Op, e.Err)
	}
	return fmt.Sprintf("target/%s: %s", e.Transport, e.Err)
}

func (e *TargetError) Unwrap() error { return e.Err }

// ValidationError represents a parameter or schema validation failure.
type ValidationError struct {
	Field   string
	Message string
	Err     error
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation: %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation: %s", e.Message)
}

func (e *ValidationError) Unwrap() error { return e.Err }

// TemplateError wraps an error from template rendering.
type TemplateError struct {
	Expression string
	Err        error
}

func (e *TemplateError) Error() string {
	if e.Expression != "" {
		return fmt.Sprintf("template %q: %s", e.Expression, e.Err)
	}
	return fmt.Sprintf("template: %s", e.Err)
}

func (e *TemplateError) Unwrap() error { return e.Err }
