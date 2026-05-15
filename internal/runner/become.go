package runner

import (
	"context"
	"maps"

	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

func canonicalizeBecome(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := cloneMap(src)
	if _, ok := dst["enabled"]; !ok {
		dst["enabled"] = true
	}
	return dst
}

func mergeBecome(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	if enabled, ok := override["enabled"].(bool); ok && !enabled {
		return canonicalizeBecome(override)
	}

	dst := cloneMap(base)
	if dst == nil {
		dst = make(map[string]any)
	}
	if len(override) > 0 {
		maps.Copy(dst, canonicalizeBecome(override))
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func resolveExecutionOptions(ctx context.Context, resolver *secrets.Resolver, source map[string]any) (map[string]any, target.ExecutionOptions, error) {
	if len(source) == 0 || resolver == nil || !resolver.HasProviders() {
		opts, err := target.NormalizeExecutionOptions(map[string]any{"become": source})
		return source, opts, err
	}

	resolved, err := resolver.ResolveMap(ctx, source)
	if err != nil {
		return nil, target.ExecutionOptions{}, err
	}
	opts, err := target.NormalizeExecutionOptions(map[string]any{"become": resolved})
	if err != nil {
		return nil, target.ExecutionOptions{}, err
	}
	return resolved, opts, nil
}
