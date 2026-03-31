package action

import (
	"context"
	"fmt"
	"strings"
)

// GitResolver fetches action definitions from remote Git repositories.
// The ref format is "github.com/org/repo@version" or "github.com/org/repo@sha".
// This implementation is a stub — full git fetch support is a future milestone.
type GitResolver struct {
	CacheDir string
}

// NewGitResolver creates a GitResolver that would cache fetched actions in
// cacheDir.
func NewGitResolver(cacheDir string) *GitResolver {
	return &GitResolver{CacheDir: cacheDir}
}

func (r *GitResolver) Name() string { return "git" }

func (r *GitResolver) Resolve(_ context.Context, ref string) (*Action, error) {
	// Only handle refs that look like remote git refs (contain a dot and @).
	if !strings.Contains(ref, ".") || !strings.Contains(ref, "@") {
		return nil, nil
	}
	return nil, fmt.Errorf("git resolver: remote action fetch not yet implemented for ref %q; "+
		"run 'preflight action fetch %s' first to cache it locally", ref, ref)
}
