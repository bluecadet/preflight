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
	return "", fmt.Errorf("template: exceeded recursive render depth")
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
// Nested maps and list items are recursively rendered.
func (e *Engine) RenderMap(m map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(m))
	for k, v := range m {
		rendered, err := e.renderValue(v)
		if err != nil {
			return nil, fmt.Errorf("template: key %q: %w", k, err)
		}
		result[k] = rendered
	}
	return result, nil
}

func (e *Engine) renderValue(v any) (any, error) {
	switch val := v.(type) {
	case string:
		return e.Render(val)
	case map[string]any:
		return e.RenderMap(val)
	case []any:
		out := make([]any, len(val))
		for i := range val {
			rendered, err := e.renderValue(val[i])
			if err != nil {
				return nil, fmt.Errorf("template: index %d: %w", i, err)
			}
			out[i] = rendered
		}
		return out, nil
	default:
		return v, nil
	}
}

// evalExpr resolves a dot-path expression such as "vars.foo.bar" against the
// engine's context. Undefined vars.* paths are errors; unknown env.*, target.*,
// and facts.* paths are treated as unresolved.
func (e *Engine) evalExpr(expr string) (string, bool, error) {
	parts := strings.Split(expr, ".")
	if len(parts) == 0 || parts[0] == "" {
		return "", false, fmt.Errorf("template: empty expression")
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
		return "", false, nil
	}

	val, err := dotLookup(root, rest)
	if err != nil {
		if namespace == "vars" {
			return "", false, fmt.Errorf("template: undefined variable %q", expr)
		}
		// Facts/target/env may legitimately be unavailable during planning.
		return "", false, nil
	}
	return fmt.Sprintf("%v", val), true, nil
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
