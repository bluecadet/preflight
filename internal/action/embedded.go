package action

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
)

const stdlibPrefix = "preflight/"

// EmbeddedResolver resolves stdlib actions from an embedded filesystem.
// It handles refs with the "preflight/" prefix, mapping them to
// actions/preflight/<name>/action.yml inside the FS.
type EmbeddedResolver struct {
	FS fs.FS
}

// NewEmbeddedResolver creates a resolver backed by the provided embedded FS.
func NewEmbeddedResolver(fsys fs.FS) *EmbeddedResolver {
	return &EmbeddedResolver{FS: fsys}
}

// Name returns a human-readable identifier for this resolver.
func (r *EmbeddedResolver) Name() string {
	return "embedded-stdlib"
}

// Resolve returns the Action for refs prefixed with "preflight/".
// Returns (nil, nil) for refs it does not handle.
func (r *EmbeddedResolver) Resolve(ctx context.Context, ref string) (*Action, error) {
	if !strings.HasPrefix(ref, stdlibPrefix) {
		return nil, nil
	}

	name := strings.TrimPrefix(ref, stdlibPrefix)
	if name == "" {
		return nil, fmt.Errorf("embedded resolver: empty action name in ref %q", ref)
	}

	path := "actions/preflight/" + name + "/action.yml"
	data, err := fs.ReadFile(r.FS, path)
	if err != nil {
		return nil, fmt.Errorf("embedded resolver: cannot read %q: %w", path, err)
	}

	action, err := ParseAction(data)
	if err != nil {
		return nil, fmt.Errorf("embedded resolver: parse %q: %w", path, err)
	}

	return action, nil
}
