package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

func inventoryFilePath(cmd *cobra.Command, projectDir string) string {
	invPath, _ := cmd.Flags().GetString("inventory")
	if invPath != "" {
		return invPath
	}
	if projectDir != "" {
		return filepath.Join(projectDir, "inventory.yml")
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "inventory.yml")
}

func resolveRunHosts(
	ctx context.Context,
	cmd *cobra.Command,
	projectDir string,
	registry target.ModuleRegistry,
	resolver *secrets.Resolver,
) ([]targeting.ResolvedHost, error) {
	selectors, _ := cmd.Flags().GetStringSlice("target")
	if len(selectors) == 0 {
		return []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, stateFilePath(cmd))}, nil
	}
	if selectorsAreLocal(selectors) {
		return []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, stateFilePath(cmd))}, nil
	}

	invPath := inventoryFilePath(cmd, projectDir)
	inv, err := inventory.ParseFile(invPath)
	if err != nil {
		return nil, fmt.Errorf("load inventory %q: %w", invPath, err)
	}

	hosts, err := targeting.ResolveHosts(ctx, inv, selectors, registry, resolver, stateFilePath(cmd))
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no hosts resolved from --target")
	}
	return hosts, nil
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan targeting.ResolvedHost)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	worker := func() {
		defer wg.Done()
		for host := range jobs {
			if err := fn(ctx, host); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}
	}

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go worker()
	}

	for _, host := range hosts {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			if firstErr != nil {
				return firstErr
			}
			return ctx.Err()
		case jobs <- host:
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
