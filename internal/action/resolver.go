package action

import (
	"context"
	"fmt"
	"strings"
)

// Resolver resolves an action ref to an Action definition.
type Resolver interface {
	// Resolve returns the Action for the given ref, or (nil, nil) if this
	// resolver does not handle the ref.
	Resolve(ctx context.Context, ref string) (*Action, error)
	// Name returns a human-readable name for this resolver (for error messages).
	Name() string
}

// Chain tries each Resolver in order, returning the first non-nil result.
// If no resolver handles the ref, it returns an error.
type Chain []Resolver

// Resolve walks the chain and returns the first non-nil Action result.
func (c Chain) Resolve(ctx context.Context, ref string) (*Action, error) {
	var attempted []string
	for _, r := range c {
		a, err := r.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("resolver %q failed for ref %q: %w", r.Name(), ref, err)
		}
		if a != nil {
			return a, nil
		}
		attempted = append(attempted, r.Name())
	}
	return nil, fmt.Errorf("action %q not found (tried: %s)", ref, strings.Join(attempted, ", "))
}
