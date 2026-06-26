package template

import (
	"maps"

	"github.com/bluecadet/preflight/internal/maputil"
)

// BindMode controls how a Scope builds a template Engine.
type BindMode int

const (
	// Bind is full apply-time mode: all namespaces (target, facts, env) are
	// expected to be present. Unknown non-var references do NOT preserve
	// their placeholder — they resolve to empty string.
	Bind BindMode = iota
	// BindPartial is plan/preview-time mode: unknown target.*, facts.*, and
	// env.* references are preserved as-is rather than replaced with an empty
	// string. vars.* references that are missing still produce an error.
	BindPartial
)

// RuntimeContext carries the runtime namespaces that are available only at
// apply time (or simulated during preview).
type RuntimeContext struct {
	Target map[string]any
	Facts  map[string]any
	Env    map[string]string
}

// Scope owns the complete variable binding environment for template
// evaluation. A Scope is built in two stages:
//
//   - Static stage (plan time): the vars layers are merged (see NewScope)
//     and composition boundaries derive a child scope (see NewDerivedScope).
//   - Binding stage (apply time): a RuntimeContext is supplied to Engine()
//     to attach the target, facts, and env namespaces.
//
// Secret handling is orthogonal and goes directly on the Engine via
// WithSecretLookup / WithPreserveSecretRefs.
//
// Vars is exported for JSON serialization of staged plans.
type Scope struct {
	Vars map[string]any `json:"Vars"`
}

// NewScope creates a root Scope by deep-merging the given var layers in
// order. Later layers win for scalars; nested maps are merged recursively.
// This implements the Layer merge semantic (ambient var precedence).
func NewScope(layers ...map[string]any) *Scope {
	merged := make(map[string]any)
	for _, layer := range layers {
		if layer != nil {
			maputil.DeepMerge(merged, layer)
		}
	}
	return &Scope{Vars: merged}
}

// NewDerivedScope creates a child Scope from a parent. The parent's vars
// are shallow-copied and the overlay is applied on top. This implements the
// Derive merge semantic (shallow overlay, composition boundary).
//
// Unlike NewScope, this does NOT deep-merge — it copies parent vars and then
// overlays new values. Keys in overlay replace parent keys wholesale (even if
// both sides are maps), matching the current behaviour of actionInputVars.
func NewDerivedScope(parent *Scope, overlay map[string]any) *Scope {
	merged := make(map[string]any, len(parent.Vars)+len(overlay))
	maps.Copy(merged, parent.Vars)
	maps.Copy(merged, overlay)
	return &Scope{Vars: merged}
}

// Engine builds a template Engine from this scope and the optional runtime
// context. The bind mode controls whether unknown non-var references
// (target.*, facts.*, env.*) are preserved or replaced with empty strings.
func (s *Scope) Engine(rt *RuntimeContext, mode BindMode) *Engine {
	eng := New(s.Vars)
	if mode == BindPartial {
		eng = eng.WithPreserveUnknown()
	}
	if rt != nil {
		if rt.Target != nil {
			eng = eng.WithTarget(rt.Target)
		}
		if rt.Facts != nil {
			eng = eng.WithFacts(rt.Facts)
		}
		if rt.Env != nil {
			eng = eng.WithEnv(rt.Env)
		}
	}
	return eng
}
