package action

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// LocalResolver resolves action refs from a local project actions directory.
// A ref like "myorg/display-config" maps to
// <BaseDir>/myorg/display-config/action.yml.
//
// Refs that look like remote URLs (contain "://", start with "github.com/", etc.)
// are not handled by this resolver.
type LocalResolver struct {
	BaseDir string
}

// NewLocalResolver creates a resolver that looks up actions under baseDir.
func NewLocalResolver(baseDir string) *LocalResolver {
	return &LocalResolver{BaseDir: baseDir}
}

// Name returns a human-readable identifier for this resolver.
func (r *LocalResolver) Name() string {
	return "local"
}

// Resolve returns the Action for a simple ref like "myorg/display-config".
// Returns (nil, nil) for refs that look like remote refs (contain a dot-separated
// hostname component, e.g. "github.com/…") or stdlib refs ("preflight/…").
func (r *LocalResolver) Resolve(ctx context.Context, ref string) (*Action, error) {
	// Skip stdlib refs — handled by EmbeddedResolver.
	if strings.HasPrefix(ref, stdlibPrefix) {
		return nil, nil
	}

	// Reject refs containing path traversal before any other processing.
	if slices.Contains(strings.Split(ref, "/"), "..") {
		return nil, fmt.Errorf("local resolver: ref %q contains path traversal", ref)
	}

	// Skip refs that look like remote hostnames (contain a '.' in the first path
	// segment) or contain '@' version specifiers.
	first := strings.SplitN(ref, "/", 2)[0]
	if strings.Contains(first, ".") || strings.Contains(ref, "@") {
		return nil, nil
	}

	actionDir := filepath.Join(r.BaseDir, filepath.FromSlash(ref))
	if !isSubPath(r.BaseDir, actionDir) {
		return nil, fmt.Errorf("local resolver: ref %q escapes base directory", ref)
	}
	path := filepath.Join(actionDir, "action.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("local resolver: read %q: %w", path, err)
	}

	action, err := ParseAction(data)
	if err != nil {
		return nil, fmt.Errorf("local resolver: parse %q: %w", path, err)
	}

	return action, nil
}
