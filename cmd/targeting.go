package cmd

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

var resolveInventoryHosts = targeting.ResolveHosts

func resolveRunHosts(
	ctx context.Context,
	cmd *cobra.Command,
	projectCfg *config.Config,
	registry target.ModuleRegistry,
	resolver *secrets.Resolver,
) ([]targeting.ResolvedHost, error) {
	selectors, _ := cmd.Flags().GetStringSlice("target")
	statePath, err := stateFilePath(cmd)
	if err != nil {
		return nil, fmt.Errorf("resolve state file path: %w", err)
	}
	if selectorsAreLocal(selectors) {
		return []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, statePath)}, nil
	}

	if projectCfg == nil || projectCfg.Inventory == nil {
		if len(selectors) > 0 {
			return nil, fmt.Errorf("no inventory configured in %s", config.FileName)
		}
		return []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, statePath)}, nil
	}

	hosts, err := resolveInventoryHosts(ctx, projectCfg.Inventory, selectors, registry, resolver, statePath)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		if len(selectors) == 0 {
			return nil, fmt.Errorf("no hosts found in inventory")
		}
		return nil, fmt.Errorf("no hosts resolved from --target")
	}
	return hosts, nil
}

// mergeSelectors combines --target flag values with positional arguments,
// deduplicating while preserving order.
func mergeSelectors(flagSelectors, positional []string) []string {
	seen := make(map[string]struct{}, len(flagSelectors)+len(positional))
	merged := make([]string, 0, len(flagSelectors)+len(positional))
	for _, s := range append(flagSelectors, positional...) {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			merged = append(merged, s)
		}
	}
	return merged
}

func selectorsAreLocal(selectors []string) bool {
	if len(selectors) == 0 {
		return false
	}
	for _, selector := range selectors {
		if !isLocalTarget(selector) {
			return false
		}
	}
	return true
}

func runHosts(
	ctx context.Context,
	hosts []targeting.ResolvedHost,
	concurrency int,
	fn func(context.Context, targeting.ResolvedHost) error,
) error {
	if len(hosts) == 0 {
		return nil
	}
	if concurrency <= 0 || concurrency > len(hosts) {
		concurrency = len(hosts)
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var joined error

hostLoop:
	for _, host := range hosts {
		select {
		case <-ctx.Done():
			mu.Lock()
			joined = errors.Join(joined, ctx.Err())
			mu.Unlock()
			break hostLoop
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(host targeting.ResolvedHost) {
			defer func() { <-sem }()
			defer wg.Done()
			// Recover here so a panic anywhere in fn (module, plugin, or
			// transport code) fails only this host instead of taking down
			// the process mid-run for every other in-flight host. The
			// recovered panic is reported through the same joined-error
			// path a normal host failure uses, so it still lands in the
			// run summary, exit status, and run log.
			//
			// This defer is registered last, so it runs first (LIFO),
			// before the wg.Done above. That ordering is required, not
			// cosmetic: the unguarded `return joined` below relies on
			// every write to joined happening-before its goroutine's
			// wg.Done call, which happens-before wg.Wait returns. Running
			// recover after wg.Done would let the caller observe Wait()
			// return and read joined while this goroutine was still
			// writing to it.
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					joined = errors.Join(joined, fmt.Errorf("host %s panicked: %v\n%s", host.Name, r, debug.Stack()))
					mu.Unlock()
				}
			}()
			if err := fn(ctx, host); err != nil {
				mu.Lock()
				joined = errors.Join(joined, err)
				mu.Unlock()
			}
		}(host)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		mu.Lock()
		joined = errors.Join(joined, err)
		mu.Unlock()
	}
	return joined
}
