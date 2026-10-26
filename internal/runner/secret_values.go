package runner

import (
	"slices"
	"strings"

	"github.com/bluecadet/preflight/internal/secrets"
	pftemplate "github.com/bluecadet/preflight/internal/template"
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
		for _, name := range pftemplate.SecretRefNames(t) {
			refs[name] = struct{}{}
		}
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
