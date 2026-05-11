package runner

import (
	"context"
	"slices"
	"strings"

	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

type SecretValueAnalysis struct {
	RefNames          []string
	HasLiteralSecrets bool
}

func AnalyzeSecretValues(value any) SecretValueAnalysis {
	refs := make(map[string]struct{})
	literal := analyzeSecretValue("", value, refs, false)
	refNames := make([]string, 0, len(refs))
	for name := range refs {
		refNames = append(refNames, name)
	}
	slices.Sort(refNames)
	return SecretValueAnalysis{
		RefNames:          refNames,
		HasLiteralSecrets: literal,
	}
}

func analyzeSecretValue(key string, value any, refs map[string]struct{}, forceSecret bool) bool {
	secretContext := forceSecret || secretishKey(key)
	switch t := value.(type) {
	case string:
		if name, ok := secretRefName(t); ok {
			refs[name] = struct{}{}
			return false
		}
		if t == "" {
			return false
		}
		return secretContext
	case map[string]any:
		literal := false
		for childKey, childValue := range t {
			if analyzeSecretValue(childKey, childValue, refs, secretContext) {
				literal = true
			}
		}
		return literal
	case []any:
		literal := false
		for _, item := range t {
			if analyzeSecretValue(key, item, refs, secretContext) {
				literal = true
			}
		}
		return literal
	case nil:
		return false
	default:
		return secretContext
	}
}

func StateParamHash(source, params, sourceBecome, become map[string]any) string {
	return hashValue(NormalizeParamsForState(source, params, sourceBecome, become))
}

func StateParamSummary(source, params, sourceBecome, become map[string]any) any {
	return NormalizeParamsForState(source, params, sourceBecome, become)
}

func NormalizeParamsForState(source, params, sourceBecome, become map[string]any) map[string]any {
	if params == nil {
		if become == nil {
			return nil
		}
	}
	sourceMap := source
	if sourceMap == nil {
		sourceMap = params
	}
	normalized := normalizeStateValue("", sourceMap, params)
	if become == nil && sourceBecome == nil {
		normalizedMap, ok := normalized.(map[string]any)
		if !ok {
			return nil
		}
		return normalizedMap
	}

	becomeSource := sourceBecome
	if becomeSource == nil {
		becomeSource = become
	}
	return map[string]any{
		"params": normalized,
		"become": normalizeStateValue("become", becomeSource, become),
	}
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

func normalizeStateValue(key string, source, resolved any) any {
	if secretishKey(key) {
		return secrets.RedactString("")
	}

	switch src := source.(type) {
	case string:
		if secrets.IsRef(src) {
			return secrets.RedactString(src)
		}
		return cloneResolvedValue(resolved)
	case map[string]any:
		resolvedMap, ok := resolved.(map[string]any)
		if !ok {
			return cloneResolvedValue(resolved)
		}
		out := make(map[string]any, len(resolvedMap))
		keys := make([]string, 0, len(resolvedMap))
		for key := range resolvedMap {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, childKey := range keys {
			childResolved := resolvedMap[childKey]
			childSource, ok := lookupSourceValue(src, childKey)
			if !ok {
				childSource = childResolved
			}
			out[childKey] = normalizeStateValue(childKey, childSource, childResolved)
		}
		return out
	case []any:
		resolvedSlice, ok := resolved.([]any)
		if !ok {
			return cloneResolvedValue(resolved)
		}
		out := make([]any, len(resolvedSlice))
		for i, item := range resolvedSlice {
			childSource := item
			if i < len(src) {
				childSource = src[i]
			}
			out[i] = normalizeStateValue(key, childSource, item)
		}
		return out
	default:
		return cloneResolvedValue(resolved)
	}
}

func lookupSourceValue(source map[string]any, key string) (any, bool) {
	if source == nil {
		return nil, false
	}
	if value, ok := source[key]; ok {
		return value, true
	}
	if fromValue, ok := source[key+"_from"]; ok {
		return fromValue, true
	}
	return nil, false
}

func cloneResolvedValue(value any) any {
	switch t := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, child := range t {
			out[key] = cloneResolvedValue(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = cloneResolvedValue(item)
		}
		return out
	default:
		return t
	}
}

func secretRefName(value string) (string, bool) {
	if !secrets.IsRef(value) {
		return "", false
	}
	parsed, err := secrets.ParseRef(value)
	if err != nil {
		return "", false
	}
	return parsed.Name, true
}

func secretishKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"password", "secret", "token", "private_key", "credential"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
