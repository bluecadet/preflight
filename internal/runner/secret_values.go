package runner

import (
	"maps"
	"slices"
	"strings"

	"github.com/bluecadet/preflight/internal/secrets"
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

func StateParamHash(source, params map[string]any) string {
	return hashValue(NormalizeParamsForState(source, params))
}

func StateParamSummary(source, params map[string]any) any {
	return NormalizeParamsForState(source, params)
}

func NormalizeParamsForState(source, params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	sourceMap := source
	if sourceMap == nil {
		sourceMap = params
	}
	normalized, ok := normalizeStateValue("", sourceMap, params).(map[string]any)
	if !ok {
		return nil
	}
	return normalized
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
		for key := range maps.Keys(resolvedMap) {
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
	for _, token := range []string{"password", "secret", "token", "private_key", "credential", "_from"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
