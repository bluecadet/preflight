package action

import (
	"context"
	"fmt"
)

// FetchResult reports the pinned lock entry and parsed action for a fetched
// remote ref.
type FetchResult struct {
	Entry  LockEntry
	Action *Action
}

// Fetcher is implemented by resolvers that can acquire remote actions into the
// local cache.
type Fetcher interface {
	Fetch(ctx context.Context, ref string) (*FetchResult, error)
}

// Fetch tries each fetch-capable resolver in order, returning the first
// non-nil fetch result.
func (c Chain) Fetch(ctx context.Context, ref string) (*FetchResult, error) {
	if !IsRemoteRef(ref) {
		return nil, nil
	}

	for _, resolver := range c {
		fetcher, ok := resolver.(Fetcher)
		if !ok {
			continue
		}
		result, err := fetcher.Fetch(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("fetcher %q failed for ref %q: %w", resolver.Name(), ref, err)
		}
		if result != nil {
			return result, nil
		}
	}

	return nil, fmt.Errorf("no fetcher registered for remote action %q", ref)
}

// FetchRefs fetches the full remote dependency closure reachable from refs.
func FetchRefs(ctx context.Context, chain Chain, refs []string) ([]LockEntry, error) {
	seen := make(map[string]bool)
	recorded := make(map[string]LockEntry)
	queue := append([]string(nil), refs...)

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ref := queue[0]
		queue = queue[1:]
		if !IsRemoteRef(ref) || seen[ref] {
			continue
		}
		seen[ref] = true

		result, err := chain.Fetch(ctx, ref)
		if err != nil {
			return nil, err
		}
		if result == nil {
			continue
		}
		recorded[result.Entry.Ref] = result.Entry
		queue = append(queue, ActionUses(result.Action)...)
	}

	entries := make([]LockEntry, 0, len(recorded))
	for _, entry := range recorded {
		entries = append(entries, entry)
	}
	return entries, nil
}
