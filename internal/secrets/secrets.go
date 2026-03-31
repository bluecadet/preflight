package secrets

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
)

const (
	// DefaultProviderName is the built-in logical provider name for repo-backed
	// secrets referenced as "secret:<name>".
	DefaultProviderName = "secret"
)

// Provider resolves named secrets to plaintext values.
type Provider interface {
	Resolve(ctx context.Context, name string) ([]byte, error)
	List() []string
}

// Ref is a parsed secret reference.
type Ref struct {
	Provider string
	Name     string
}

// Resolver resolves secret references through named providers.
type Resolver struct {
	providers map[string]Provider
}

// NewResolver builds a Resolver from the given providers map.
func NewResolver(providers map[string]Provider) *Resolver {
	if providers == nil {
		providers = make(map[string]Provider)
	}
	return &Resolver{providers: providers}
}

// ParseRef parses ref strings in the form "provider:name".
func ParseRef(ref string) (Ref, error) {
	provider, name, ok := strings.Cut(ref, ":")
	if !ok || provider == "" || name == "" {
		return Ref{}, fmt.Errorf("secret reference %q must use provider:name syntax", ref)
	}
	return Ref{
		Provider: provider,
		Name:     name,
	}, nil
}

// IsRef reports whether s looks like a secret reference.
func IsRef(s string) bool {
	ref, err := ParseRef(s)
	return err == nil && ref.Provider != ""
}

// ResolveRef resolves a single secret reference to plaintext.
func (r *Resolver) ResolveRef(ctx context.Context, ref string) (string, error) {
	parsed, err := ParseRef(ref)
	if err != nil {
		return "", err
	}
	provider, ok := r.providers[parsed.Provider]
	if !ok {
		return "", fmt.Errorf("secret provider %q is not configured", parsed.Provider)
	}
	data, err := provider.Resolve(ctx, parsed.Name)
	if err != nil {
		return "", RedactError(fmt.Errorf("resolve %q: %w", ref, err))
	}
	return string(data), nil
}

// ResolveValue recursively resolves secret references anywhere within v.
func (r *Resolver) ResolveValue(ctx context.Context, v any) (any, error) {
	switch t := v.(type) {
	case string:
		if !IsRef(t) {
			return t, nil
		}
		return r.ResolveRef(ctx, t)
	case map[string]any:
		return r.ResolveMap(ctx, t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			resolved, err := r.ResolveValue(ctx, item)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}

// ResolveMap resolves secret references throughout params.
// Keys ending in "_from" are additionally copied to their base field names so
// modules can continue reading their existing plaintext keys.
func (r *Resolver) ResolveMap(ctx context.Context, params map[string]any) (map[string]any, error) {
	if params == nil {
		return nil, nil
	}
	resolved := make(map[string]any, len(params))
	for key, value := range params {
		out, err := r.ResolveValue(ctx, value)
		if err != nil {
			return nil, err
		}
		resolved[key] = out
	}
	for key, value := range params {
		if !strings.HasSuffix(key, "_from") {
			continue
		}
		base := strings.TrimSuffix(key, "_from")
		out, err := r.ResolveValue(ctx, value)
		if err != nil {
			return nil, err
		}
		resolved[base] = out
	}
	return resolved, nil
}

// HasProviders reports whether any providers are configured.
func (r *Resolver) HasProviders() bool {
	return r != nil && len(r.providers) > 0
}

// ProviderNames returns configured provider names in deterministic order.
func (r *Resolver) ProviderNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.providers))
	for name := range maps.Keys(r.providers) {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RedactString returns a fixed marker for secret-bearing values.
func RedactString(_ string) string {
	return "[redacted]"
}

// RedactError strips secret values from error surfaces by replacing the
// message with a generic secrets-oriented error string.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("secret resolution failed")
}
