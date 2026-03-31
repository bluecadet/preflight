package template

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// exprRe matches {{ ... }} expressions, capturing the inner expression.
// It tolerates optional whitespace inside the delimiters.
var exprRe = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// Engine evaluates {{ expression }} placeholders in strings using a simple
// dot-path resolver. Supported namespaces: vars, env, target, facts.
//
// Unknown keys resolve to an empty string by default. An error is returned
// only when the expression syntax is invalid (e.g. an empty path segment).
type Engine struct {
	vars   map[string]any
	env    map[string]string
	target map[string]any
	facts  map[string]any
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

// Render evaluates all {{ expression }} placeholders in s and returns the
// resulting string. Unknown variable paths resolve to an empty string.
func (e *Engine) Render(s string) (string, error) {
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
		val, err := e.evalExpr(expr)
		if err != nil {
			renderErr = err
			return match
		}
		return val
	})
	if renderErr != nil {
		return "", renderErr
	}
	return result, nil
}

// RenderBool renders s and parses the result as a boolean.
// Accepted truthy strings: "true", "1", "yes". Everything else is false.
func (e *Engine) RenderBool(s string) (bool, error) {
	rendered, err := e.Render(s)
	if err != nil {
		return false, err
	}
	rendered = strings.TrimSpace(strings.ToLower(rendered))
	b, err := strconv.ParseBool(rendered)
	if err != nil {
		// Accept "yes" / "no" as extensions
		switch rendered {
		case "yes":
			return true, nil
		case "no":
			return false, nil
		}
		return false, fmt.Errorf("template: cannot parse %q as bool", rendered)
	}
	return b, nil
}

// RenderMap renders all string values in m and returns a new map.
// Non-string values are passed through unchanged.
// Nested map[string]any values are recursively rendered.
func (e *Engine) RenderMap(m map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			rendered, err := e.Render(val)
			if err != nil {
				return nil, fmt.Errorf("template: key %q: %w", k, err)
			}
			result[k] = rendered
		case map[string]any:
			nested, err := e.RenderMap(val)
			if err != nil {
				return nil, err
			}
			result[k] = nested
		default:
			result[k] = v
		}
	}
	return result, nil
}

// evalExpr resolves a dot-path expression such as "vars.foo.bar" against the
// engine's context. Returns empty string for unknown paths.
func (e *Engine) evalExpr(expr string) (string, error) {
	parts := strings.Split(expr, ".")
	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("template: empty expression")
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
		// Unknown namespace — return empty string (not an error).
		return "", nil
	}

	val, err := dotLookup(root, rest)
	if err != nil {
		// Unresolvable path — return empty string.
		return "", nil
	}
	return fmt.Sprintf("%v", val), nil
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
