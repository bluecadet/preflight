package runner

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/action"
)

// fetch downloads remote action refs not yet in cache.
func (r *Runner) fetch(ctx context.Context, playbook *action.Playbook) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if playbook == nil {
		return fmt.Errorf("fetch: nil playbook")
	}

	_, err := action.FetchRefs(ctx, r.resolver, action.PlaybookUses(playbook))
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}
