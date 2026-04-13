package template

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/bluecadet/preflight/internal/preflighterr"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
)

const maxRenderPasses = 16

const (
	lookupFuncName = "__preflight_lookup"
	truthyFuncName = "__preflight_truthy"
	eqFuncName     = "__preflight_eq"
	neqFuncName    = "__preflight_neq"
	gtFuncName     = "__preflight_gt"
	gteFuncName    = "__preflight_gte"
	ltFuncName     = "__preflight_lt"
	lteFuncName    = "__preflight_lte"
)

// exprRe matches {{ ... }} expressions, capturing the inner expression.
// It tolerates optional whitespace inside the delimiters.
var exprRe = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// Engine evaluates {{ expression }} placeholders in strings using an
// expression engine with custom lookup semantics for vars/env/target/facts.
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

type unresolvedValue struct{}

type exprCompatPatcher struct{}

type namespaceIdentifierPatcher struct{}

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
		inner := exprRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		expression := strings.TrimSpace(inner[1])
		value, resolved, err := e.evalExprValue(expression)
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
		return fmt.Sprintf("%v", value)
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

// evalExprValue returns the raw typed value for an expression.
func (e *Engine) evalExprValue(expression string) (any, bool, error) {
	program, err := expr.Compile(expression, e.compileOptions()...)
	if err != nil {
		return nil, false, err
	}

	value, err := expr.Run(program, map[string]any{})
	if err != nil {
		return nil, false, err
	}
	if isUnresolved(value) {
		return nil, false, nil
	}
	return value, true, nil
}

func (e *Engine) compileOptions() []expr.Option {
	return []expr.Option{
		expr.Env(map[string]any{}),
		expr.AsAny(),
		expr.AllowUndefinedVariables(),
		expr.Patch(&exprCompatPatcher{}),
		expr.Patch(&namespaceIdentifierPatcher{}),
		expr.Function(lookupFuncName, e.lookupExprFunc, new(func(string) any)),
		expr.Function(truthyFuncName, truthyExprFunc, new(func(any) bool)),
		expr.Function(eqFuncName, eqExprFunc, new(func(any, any) bool)),
		expr.Function(neqFuncName, neqExprFunc, new(func(any, any) bool)),
		expr.Function(gtFuncName, gtExprFunc, new(func(any, any) bool)),
		expr.Function(gteFuncName, gteExprFunc, new(func(any, any) bool)),
		expr.Function(ltFuncName, ltExprFunc, new(func(any, any) bool)),
		expr.Function(lteFuncName, lteExprFunc, new(func(any, any) bool)),
		expr.Operator("==", eqFuncName),
		expr.Operator("!=", neqFuncName),
		expr.Operator(">", gtFuncName),
		expr.Operator(">=", gteFuncName),
		expr.Operator("<", ltFuncName),
		expr.Operator("<=", lteFuncName),
	}
}

func (e *Engine) lookupExprFunc(args ...any) (any, error) {
	if len(args) != 1 {
		return nil, &preflighterr.TemplateError{Err: errors.New("lookup expects exactly one argument")}
	}
	path, ok := args[0].(string)
	if !ok {
		return nil, &preflighterr.TemplateError{Err: errors.New("lookup path must be a string")}
	}
	return e.lookupPath(path)
}

func truthyExprFunc(args ...any) (any, error) {
	if len(args) != 1 {
		return nil, &preflighterr.TemplateError{Err: errors.New("truthy expects exactly one argument")}
	}
	return !isFalsyValue(args[0]), nil
}

func eqExprFunc(args ...any) (any, error) {
	if len(args) != 2 {
		return nil, &preflighterr.TemplateError{Err: errors.New("== expects exactly two operands")}
	}
	if isUnresolved(args[0]) || isUnresolved(args[1]) {
		return false, nil
	}
	return fmt.Sprintf("%v", args[0]) == fmt.Sprintf("%v", args[1]), nil
}

func neqExprFunc(args ...any) (any, error) {
	result, err := eqExprFunc(args...)
	if err != nil {
		return nil, err
	}
	return !result.(bool), nil
}

func gtExprFunc(args ...any) (any, error) {
	return numericCompare(">", args...)
}

func gteExprFunc(args ...any) (any, error) {
	return numericCompare(">=", args...)
}

func ltExprFunc(args ...any) (any, error) {
	return numericCompare("<", args...)
}

func lteExprFunc(args ...any) (any, error) {
	return numericCompare("<=", args...)
}

func numericCompare(op string, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, &preflighterr.TemplateError{Err: fmt.Errorf("%s expects exactly two operands", op)}
	}
	if isUnresolved(args[0]) || isUnresolved(args[1]) {
		return false, nil
	}

	left, err := coerceFloat(args[0])
	if err != nil {
		return false, &preflighterr.TemplateError{Err: fmt.Errorf("numeric comparison %q: left operand %q is not a number", op, fmt.Sprintf("%v", args[0]))}
	}
	right, err := coerceFloat(args[1])
	if err != nil {
		return false, &preflighterr.TemplateError{Err: fmt.Errorf("numeric comparison %q: right operand %q is not a number", op, fmt.Sprintf("%v", args[1]))}
	}

	switch op {
	case ">":
		return left > right, nil
	case ">=":
		return left >= right, nil
	case "<":
		return left < right, nil
	case "<=":
		return left <= right, nil
	default:
		return nil, &preflighterr.TemplateError{Err: fmt.Errorf("unknown comparison operator %q", op)}
	}
}

func coerceFloat(v any) (float64, error) {
	return strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
}

func isFalsyValue(v any) bool {
	if isUnresolved(v) {
		return true
	}
	return isFalsy(fmt.Sprintf("%v", v))
}

func isFalsy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "false", "0", "no":
		return true
	default:
		return false
	}
}

func isUnresolved(v any) bool {
	_, ok := v.(unresolvedValue)
	return ok
}

func (e *Engine) lookupPath(path string) (any, error) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 || parts[0] == "" {
		return nil, fmt.Errorf("empty template expression")
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
		return unresolvedValue{}, nil
	}

	value, err := dotLookup(root, rest)
	if err != nil {
		if namespace == "vars" {
			return nil, fmt.Errorf("undefined variable %q", path)
		}
		return unresolvedValue{}, nil
	}
	return value, nil
}

func dotLookup(root any, path []string) (any, error) {
	if len(path) == 0 {
		return root, nil
	}

	key := path[0]
	if key == "" {
		return nil, &preflighterr.TemplateError{Err: errors.New("empty path segment")}
	}

	switch m := root.(type) {
	case map[string]any:
		value, ok := m[key]
		if !ok {
			return nil, &preflighterr.TemplateError{Err: fmt.Errorf("key %q not found", key)}
		}
		return dotLookup(value, path[1:])
	case map[string]string:
		value, ok := m[key]
		if !ok {
			return nil, &preflighterr.TemplateError{Err: fmt.Errorf("key %q not found", key)}
		}
		if len(path) > 1 {
			return nil, &preflighterr.TemplateError{Err: fmt.Errorf("cannot traverse into string value at %q", key)}
		}
		return value, nil
	default:
		return nil, &preflighterr.TemplateError{Err: fmt.Errorf("cannot index into %T", root)}
	}
}

func mapToIface(m map[string]any) any {
	return m
}

func envToIface(m map[string]string) any {
	return m
}

func (p *exprCompatPatcher) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.MemberNode:
		if path, ok := collectMemberPath(n); ok {
			ast.Patch(node, lookupCallNode(path))
		}
	case *ast.ConditionalNode:
		if !isTruthyCall(n.Cond) {
			n.Cond = truthyCallNode(n.Cond)
		}
	}
}

func (p *namespaceIdentifierPatcher) Visit(node *ast.Node) {
	ident, ok := (*node).(*ast.IdentifierNode)
	if !ok {
		return
	}
	if !isNamespaceIdentifier(ident.Value) {
		return
	}
	ast.Patch(node, lookupCallNode(ident.Value))
}

func collectMemberPath(node *ast.MemberNode) (string, bool) {
	if node.Method {
		return "", false
	}

	property, ok := node.Property.(*ast.StringNode)
	if !ok {
		return "", false
	}

	base, ok := collectPathNode(node.Node)
	if !ok {
		return "", false
	}
	return base + "." + property.Value, true
}

func collectPathNode(node ast.Node) (string, bool) {
	switch n := node.(type) {
	case *ast.IdentifierNode:
		return n.Value, true
	case *ast.MemberNode:
		return collectMemberPath(n)
	case *ast.CallNode:
		return lookupPathFromCall(n)
	default:
		return "", false
	}
}

func lookupPathFromCall(node *ast.CallNode) (string, bool) {
	ident, ok := node.Callee.(*ast.IdentifierNode)
	if !ok || ident.Value != lookupFuncName || len(node.Arguments) != 1 {
		return "", false
	}
	path, ok := node.Arguments[0].(*ast.StringNode)
	if !ok {
		return "", false
	}
	return path.Value, true
}

func lookupCallNode(path string) ast.Node {
	return &ast.CallNode{
		Callee: &ast.IdentifierNode{Value: lookupFuncName},
		Arguments: []ast.Node{
			&ast.StringNode{Value: path},
		},
	}
}

func truthyCallNode(node ast.Node) ast.Node {
	return &ast.CallNode{
		Callee: &ast.IdentifierNode{Value: truthyFuncName},
		Arguments: []ast.Node{
			node,
		},
	}
}

func isTruthyCall(node ast.Node) bool {
	call, ok := node.(*ast.CallNode)
	if !ok {
		return false
	}
	ident, ok := call.Callee.(*ast.IdentifierNode)
	return ok && ident.Value == truthyFuncName
}

func isNamespaceIdentifier(value string) bool {
	switch value {
	case "vars", "env", "target", "facts":
		return true
	default:
		return false
	}
}
