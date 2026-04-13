package preflighterr

import (
	"errors"
	"testing"
)

func TestModuleErrorWrapping(t *testing.T) {
	innerErr := errors.New("test error")
	modErr := &ModuleError{
		Module: "testmod",
		Op:     "check",
		Err:    innerErr,
	}

	// Test Error() method
	errStr := modErr.Error()
	if errStr != "module \"testmod\": check: test error" {
		t.Errorf("ModuleError.Error() = %q, want %q", errStr, "module \"testmod\": check: test error")
	}

	// Test Unwrap() method
	if modErr.Unwrap() != innerErr {
		t.Error("ModuleError.Unwrap() did not return inner error")
	}

	// Test errors.Is chain
	if !errors.Is(modErr, innerErr) {
		t.Error("errors.Is(modErr, innerErr) should be true")
	}
}

func TestModuleErrorWithoutOp(t *testing.T) {
	innerErr := errors.New("test error")
	modErr := &ModuleError{
		Module: "testmod",
		Err:    innerErr,
	}

	errStr := modErr.Error()
	if errStr != "module \"testmod\": test error" {
		t.Errorf("ModuleError.Error() without Op = %q, want %q", errStr, "module \"testmod\": test error")
	}
}

func TestTargetErrorWrapping(t *testing.T) {
	innerErr := errors.New("connection failed")
	targetErr := &TargetError{
		Transport: "local",
		Op:        "copy",
		Err:       innerErr,
	}

	errStr := targetErr.Error()
	if errStr != "target/local: copy: connection failed" {
		t.Errorf("TargetError.Error() = %q, want %q", errStr, "target/local: copy: connection failed")
	}

	if targetErr.Unwrap() != innerErr {
		t.Error("TargetError.Unwrap() did not return inner error")
	}

	if !errors.Is(targetErr, innerErr) {
		t.Error("errors.Is(targetErr, innerErr) should be true")
	}
}

func TestTargetErrorWithoutOp(t *testing.T) {
	innerErr := errors.New("connection failed")
	targetErr := &TargetError{
		Transport: "local",
		Err:       innerErr,
	}

	errStr := targetErr.Error()
	if errStr != "target/local: connection failed" {
		t.Errorf("TargetError.Error() without Op = %q, want %q", errStr, "target/local: connection failed")
	}
}

func TestValidationErrorWrapping(t *testing.T) {
	innerErr := errors.New("invalid format")
	valErr := &ValidationError{
		Field:   "timeout",
		Message: "must be positive",
		Err:     innerErr,
	}

	errStr := valErr.Error()
	if errStr != "validation: timeout: must be positive" {
		t.Errorf("ValidationError.Error() = %q, want %q", errStr, "validation: timeout: must be positive")
	}

	if valErr.Unwrap() != innerErr {
		t.Error("ValidationError.Unwrap() did not return inner error")
	}

	if !errors.Is(valErr, innerErr) {
		t.Error("errors.Is(valErr, innerErr) should be true")
	}
}

func TestValidationErrorWithoutField(t *testing.T) {
	innerErr := errors.New("invalid format")
	valErr := &ValidationError{
		Message: "must be positive",
		Err:     innerErr,
	}

	errStr := valErr.Error()
	if errStr != "validation: must be positive" {
		t.Errorf("ValidationError.Error() without Field = %q, want %q", errStr, "validation: must be positive")
	}
}

func TestTemplateErrorWrapping(t *testing.T) {
	innerErr := errors.New("undefined variable")
	tmplErr := &TemplateError{
		Expression: "{{ vars.missing }}",
		Err:        innerErr,
	}

	errStr := tmplErr.Error()
	if errStr != "template \"{{ vars.missing }}\": undefined variable" {
		t.Errorf("TemplateError.Error() = %q, want %q", errStr, "template \"{{ vars.missing }}\": undefined variable")
	}

	if tmplErr.Unwrap() != innerErr {
		t.Error("TemplateError.Unwrap() did not return inner error")
	}

	if !errors.Is(tmplErr, innerErr) {
		t.Error("errors.Is(tmplErr, innerErr) should be true")
	}
}

func TestTemplateErrorWithoutExpression(t *testing.T) {
	innerErr := errors.New("undefined variable")
	tmplErr := &TemplateError{
		Err: innerErr,
	}

	errStr := tmplErr.Error()
	if errStr != "template: undefined variable" {
		t.Errorf("TemplateError.Error() without Expression = %q, want %q", errStr, "template: undefined variable")
	}
}

func TestErrorChaining(t *testing.T) {
	innerErr := errors.New("root cause")
	modErr := &ModuleError{
		Module: "testmod",
		Op:     "check",
		Err:    innerErr,
	}

	// The chain should work
	if !errors.Is(modErr, innerErr) {
		t.Error("errors.Is should work through ModuleError")
	}
}
