package action

import (
	"os"
	"path/filepath"

	"github.com/bluecadet/preflight/internal/stdlib"
)

// DefaultChain builds the standard resolver chain:
//
//	embedded stdlib → local ./actions/ → user cache → git (clones and pins remote refs)
func DefaultChain(projectDir string) Chain {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".preflight", "actions")
	lockfilePath := filepath.Join(projectDir, LockfileName)

	return Chain{
		NewEmbeddedResolver(stdlib.FS),
		NewLocalResolver(filepath.Join(projectDir, "actions")),
		NewCacheResolver(cacheDir),
		NewGitResolver(cacheDir, lockfilePath),
	}
}
