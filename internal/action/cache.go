package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CacheResolver resolves versioned action refs from the local user cache at
// ~/.preflight/actions/. Refs of the form "github.com/org/name@v1.2" map to
// <cacheDir>/github.com/org/name@v1.2/action.yml.
type CacheResolver struct {
	CacheDir string
}

// NewCacheResolver creates a CacheResolver. If cacheDir is empty it defaults
// to ~/.preflight/actions/.
func NewCacheResolver(cacheDir string) *CacheResolver {
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			cacheDir = filepath.Join(home, ".preflight", "actions")
		}
	}
	return &CacheResolver{CacheDir: cacheDir}
}

func (r *CacheResolver) Name() string { return "cache" }

func (r *CacheResolver) Resolve(_ context.Context, ref string) (*Action, error) {
	if r.CacheDir == "" {
		return nil, nil
	}
	// Only handle refs that contain "@" (versioned remote refs).
	if !strings.Contains(ref, "@") {
		return nil, nil
	}
	action, err := loadActionFromCache(r.CacheDir, ref)
	if err != nil {
		return nil, fmt.Errorf("cache resolver: %w", err)
	}
	return action, nil
}
