package template

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const maxRenderPasses = 16

// exprRe matches {{ ... }} expressions, capturing the inner expression.
// It tolerates optional whitespace inside the delimiters.
var exprRe = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// Engine evaluates {{ expression }} placeholders in strings using a simple
// dot-path resolver. Supported namespaces: vars, env, target, facts.
//
// Unknown vars.* keys return an error so missing configuration is caught early.
// Unknown env.*, target.*, and facts.* keys resolve to an empty string by
// default unless preserveUnknown is enabled.
type Engine struct {
	vars   map[string]any
	env    map[string]string
	target map[string]any
	facts  map[string]any

	preserveUnknown bool
}

// New creates an Engine pre-loaded with the merged variable map.
func New(vars map[string]any) *Engine {
	if vars == nil {
		vars = make(map[string]any)
	}
	return &Engine{
		vars:   vars,
		env:    make(map[string]string),
		target: make(map[string]any),
		facts:  make(map[string]any),
	}
}

// WithEnv attaches an environment variable map to the engine and returns it
// (fluent API).
func (e *Engine) WithEnv(env map[string]string) *Engine {
	if env != nil {
		e.env = env
	}
	return e
}

// WithTarget attaches target-info fields and returns the engine.
func (e *Engine) WithTarget(target map[string]any) *Engine {
	if target != nil {
		e.target = target
	}
	return e
}

// WithFacts attaches gathered facts and returns the engine.
func (e *Engine) WithFacts(facts map[string]any) *Engine {
	if facts != nil {
		e.facts = facts
	}
	return e
}

// WithPreserveUnknown keeps unknown env.*, target.*, and facts.* expressions
// intact instead of replacing them with an empty string. This is useful during
// pure planning when facts or target metadata may not be available yet.
// Undefined vars.* references still return an error.
func (e *Engine) WithPreserveUnknown() *Engine {
	e.preserveUnknown = true
	return e
}

// Render evaluates all {{ expression }} placeholders in s and returns the
// resulting string. Undefined vars.* references return an error.
func (e *Engine) Render(s string) (string, error) {
	current := s
	seen := map[string]struct{}{current: {}}
	for range maxRenderPasses {
		rendered, err := e.renderOnce(current)
		if err != nil {
			return "", err
		}
		if rendered == current {
			return rendered, nil
		}
		if _, ok := seen[rendered]; ok {
			return rendered, nil
		}
		seen[rendered] = struct{}{}
		current = rendered
	}
	return "", fmt.Errorf("exceeded recursive render depth")
}

func (e *Engine) renderOnce(s string) (string, error) {
	var renderErr error
	result := exprRe.ReplaceAllStringFunc(s, func(match string) string {
		if renderErr != nil {
			return match
		}
		// Extract inner expression (strip delimiters + whitespace)
		inner := exprRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		expr := strings.TrimSpace(inner[1])
		val, resolved, err := e.evalExpr(expr)
		if err != nil {
			renderErr = err
			return match
		}
		if !resolved {
			if e.preserveUnknown {
				return match
			}
			return ""
		}
		return val
	})
	if renderErr != nil {
		return "", renderErr
	}
	return result, nil
}

// RenderBool renders s and interprets the result as a boolean using truthy
// semantics. Empty string and explicit false values ("false", "0", "no") are
// false; everything else is true. This allows when: conditions to gate on
// optional string inputs — an empty or unset var is falsy, a non-empty value
// (including rendered template expressions) is truthy.
func (e *Engine) RenderBool(s string) (bool, error) {
	rendered, err := e.Render(s)
	if err != nil {
		return false, err
	}
	rendered = strings.TrimSpace(strings.ToLower(rendered))
	switch rendered {
	case "", "false", "0", "no":
		return false, nil
	default:
		return true, nil
	}
}

// RenderMap renders all string values in m and returns a new map.
// Non-string values are passed through unchanged.
// Nested maps and list items are recursively rendered.
func (e *Engine) RenderMap(m map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(m))
	for k, v := range m {
		rendered, err := e.renderValue(v)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		result[k] = rendered
	}
	return result, nil
}

func (e *Engine) renderValue(v any) (any, error) {
	switch val := v.(type) {
	case string:
		// Whole-value substitution: if the entire string is a single {{ expr }},
		// return the actual typed value rather than stringifying. This lets
		// non-string vars (lists, maps, booleans) flow through action inputs
		// without being coerced to their string representation.
		// If the resolved value is itself a string, fall through to normal
		// rendering so recursive variable references still work.
		if m := exprRe.FindStringIndex(val); m != nil && m[0] == 0 && m[1] == len(val) {
			inner := strings.TrimSpace(exprRe.FindStringSubmatch(val)[1])
			raw, resolved, err := e.evalExprValue(inner)
			if err != nil {
				return nil, err
			}
			if resolved {
				if strVal, ok := raw.(string); ok {
					return e.Render(strVal)
				}
				// Only bypass stringification for structural types (lists, maps).
				// Scalars (int, bool, float64, etc.) still stringify so that
				// `ac_value: "{{ vars.count }}"` keeps behaving as a string field.
				switch raw.(type) {
				case map[string]any, []any:
					return raw, nil
				default:
					return fmt.Sprintf("%v", raw), nil
				}
			}
			if e.preserveUnknown {
				return val, nil
			}
			return "", nil
		}
		return e.Render(val)
	case map[string]any:
		return e.RenderMap(val)
	case []any:
		out := make([]any, len(val))
		for i := range val {
			rendered, err := e.renderValue(val[i])
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			out[i] = rendered
		}
		return out, nil
	default:
		return v, nil
	}
}

// evalExpr resolves a dot-path expression such as "vars.foo.bar" against the
// engine's context and returns its string representation. Undefined vars.*
// paths are errors; unknown env.*, target.*, and facts.* paths are unresolved.
func (e *Engine) evalExpr(expr string) (string, bool, error) {
	val, resolved, err := e.evalExprValue(expr)
	if !resolved || err != nil {
		return "", resolved, err
	}
	return fmt.Sprintf("%v", val), true, nil
}

// evalExprValue is like evalExpr but returns the raw typed value without
// stringifying it, enabling whole-value substitution for non-string types.
//
// Comparison operators are evaluated before dot-path lookup. Supported
// operators: ==, !=, >=, <=, >, <.
//
// Coercion rules:
//   - == and !=: both operands rendered to strings, then compared.
//   - >, >=, <, <=: both operands coerced to float64; error if not numeric.
func (e *Engine) evalExprValue(expr string) (any, bool, error) {
	// Check for comparison operators (longest first to avoid mis-splitting on >=/>).
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		lhsRaw, rhsRaw, found := strings.Cut(expr, op)
		if !found {
			continue
		}
		lhsExpr := strings.TrimSpace(lhsRaw)
		rhsExpr := strings.TrimSpace(rhsRaw)

		lhsStr, lhsResolved, err := e.evalOperand(lhsExpr)
		if err != nil {
			return nil, false, err
		}
		rhsStr, rhsResolved, err := e.evalOperand(rhsExpr)
		if err != nil {
			return nil, false, err
		}

		// If either operand is an unresolved non-vars reference treat as false.
		if !lhsResolved || !rhsResolved {
			return false, true, nil
		}

		result, err := evalComparison(op, lhsStr, rhsStr)
		if err != nil {
			return nil, false, err
		}
		return result, true, nil
	}

	parts := strings.Split(expr, ".")
	if len(parts) == 0 || parts[0] == "" {
		return nil, false, fmt.Errorf("empty template expression")
	}

	namespace := parts[0]
	rest := parts[1:]

	var root any
	switch namespace {
	case "vars":
		root = mapToIface(e.vars)
	case "env":
		root = envToIface(e.env)
	case "target":
		root = mapToIface(e.target)
	case "facts":
		root = mapToIface(e.facts)
	default:
		return nil, false, nil
	}

	val, err := dotLookup(root, rest)
	if err != nil {
		if namespace == "vars" {
			return nil, false, fmt.Errorf("undefined variable %q", expr)
		}
		return nil, false, nil
	}
	return val, true, nil
}

// evalOperand resolves a single comparison operand. It renders dot-path
// expressions and strips surrounding single or double quotes from literals.
// Bare numeric literals (e.g. 5 or 3.14) are returned as-is so numeric
// comparisons work. Unresolved dot-path expressions propagate resolved=false.
func (e *Engine) evalOperand(operand string) (string, bool, error) {
	// Quoted string literal.
	if (strings.HasPrefix(operand, "'") && strings.HasSuffix(operand, "'")) ||
		(strings.HasPrefix(operand, `"`) && strings.HasSuffix(operand, `"`)) {
		return operand[1 : len(operand)-1], true, nil
	}
	// Dot-path expression (must start with a known namespace).
	val, resolved, err := e.evalExprValue(operand)
	if err != nil {
		return "", false, err
	}
	if resolved {
		return fmt.Sprintf("%v", val), true, nil
	}
	// Bare numeric literal (e.g. 5, 3.14). Only treat as resolved if it
	// parses as a number; unknown namespaces or unresolved refs propagate false.
	if _, err := strconv.ParseFloat(operand, 64); err == nil {
		return operand, true, nil
	}
	return operand, false, nil
}

// evalComparison applies op to two already-rendered string operands.
func evalComparison(op, lhs, rhs string) (bool, error) {
	switch op {
	case "==":
		return lhs == rhs, nil
	case "!=":
		return lhs != rhs, nil
	}
	// Numeric operators.
	l, err := strconv.ParseFloat(lhs, 64)
	if err != nil {
		return false, fmt.Errorf("template: numeric comparison %q: left operand %q is not a number", op, lhs)
	}
	r, err := strconv.ParseFloat(rhs, 64)
	if err != nil {
		return false, fmt.Errorf("template: numeric comparison %q: right operand %q is not a number", op, rhs)
	}
	switch op {
	case ">":
		return l > r, nil
	case ">=":
		return l >= r, nil
	case "<":
		return l < r, nil
	case "<=":
		return l <= r, nil
	default:
		return false, fmt.Errorf("template: unknown comparison operator %q", op)
	}
}

// dotLookup traverses a nested map structure following the given path segments.
func dotLookup(root any, path []string) (any, error) {
	if len(path) == 0 {
		return root, nil
	}
	key := path[0]
	if key == "" {
		return nil, fmt.Errorf("template: empty path segment")
	}
	switch m := root.(type) {
	case map[string]any:
		val, ok := m[key]
		if !ok {
			return nil, fmt.Errorf("template: key %q not found", key)
		}
		return dotLookup(val, path[1:])
	case map[string]string:
		val, ok := m[key]
		if !ok {
			return nil, fmt.Errorf("template: key %q not found", key)
		}
		if len(path) > 1 {
			return nil, fmt.Errorf("template: cannot traverse into string value at %q", key)
		}
		return val, nil
	default:
		return nil, fmt.Errorf("template: cannot index into %T", root)
	}
}

func mapToIface(m map[string]any) any {
	return m
}

func envToIface(m map[string]string) any {
	return m
}
