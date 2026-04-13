package action

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	schemafiles "github.com/bluecadet/preflight/schema"
)

const (
	actionSchemaURL   = "https://preflight.dev/schema/action.schema.json"
	playbookSchemaURL = "https://preflight.dev/schema/playbook.schema.json"
)

var (
	schemaOnce      sync.Once
	schemaCache     map[string]*jsonschema.Schema
	schemaCacheErr  error
	schemaResources = []struct {
		url  string
		path string
	}{
		{url: actionSchemaURL, path: "action.schema.json"},
		{url: playbookSchemaURL, path: "playbook.schema.json"},
	}
)

// ValidateActionYAML validates an action document against the embedded JSON schema.
func ValidateActionYAML(data []byte) error {
	return validateYAMLDocument(data, actionSchemaURL)
}

// ValidateActionDocument validates an action value against the action schema.
func ValidateActionDocument(doc any) error {
	return validateDocument(doc, actionSchemaURL)
}

// ValidatePlaybookYAML validates a playbook document against the embedded JSON schema.
func ValidatePlaybookYAML(data []byte) error {
	return validateYAMLDocument(data, playbookSchemaURL)
}

// ValidatePlaybookDocument validates a playbook value against the playbook schema.
func ValidatePlaybookDocument(doc any) error {
	return validateDocument(doc, playbookSchemaURL)
}

func validateYAMLDocument(data []byte, schemaURL string) error {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("schema validation parse error: %w", err)
	}

	return validateDocument(doc, schemaURL)
}

func validateDocument(doc any, schemaURL string) error {
	normalized, err := normalizeDocument(doc)
	if err != nil {
		return fmt.Errorf("schema validation parse error: %w", err)
	}

	schemas, err := compiledSchemas()
	if err != nil {
		return fmt.Errorf("schema validation setup error: %w", err)
	}

	if err := schemas[schemaURL].Validate(normalized); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

func compiledSchemas() (map[string]*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)

		schemaCache = make(map[string]*jsonschema.Schema, len(schemaResources))
		for _, resource := range schemaResources {
			data, err := schemafiles.FS.ReadFile(resource.path)
			if err != nil {
				schemaCacheErr = err
				return
			}

			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if err != nil {
				schemaCacheErr = fmt.Errorf("load %s: %w", resource.path, err)
				return
			}
			if err := compiler.AddResource(resource.url, allowTemplateExpressionsInSchema(doc)); err != nil {
				schemaCacheErr = fmt.Errorf("add %s: %w", resource.path, err)
				return
			}
		}

		for _, resource := range schemaResources {
			schemaCache[resource.url], schemaCacheErr = compiler.Compile(resource.url)
			if schemaCacheErr != nil {
				schemaCacheErr = fmt.Errorf("compile %s: %w", resource.path, schemaCacheErr)
				return
			}
		}
	})
	return schemaCache, schemaCacheErr
}

func normalizeDocument(doc any) (any, error) {
	switch doc.(type) {
	case *Action, Action, *Playbook, Playbook:
		data, err := yaml.Marshal(doc)
		if err != nil {
			return nil, err
		}

		var decoded any
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			return nil, err
		}
		return normalizeYAMLValue(decoded)
	default:
		return normalizeYAMLValue(doc)
	}
}

func normalizeYAMLValue(v any) (any, error) {
	switch typed := v.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return typed, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			normalized, err := normalizeYAMLValue(value)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			stringKey, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("non-string object key %T", key)
			}
			normalized, err := normalizeYAMLValue(value)
			if err != nil {
				return nil, err
			}
			out[stringKey] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			normalized, err := normalizeYAMLValue(value)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return typed, nil
	}
}

func allowTemplateExpressionsInSchema(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, value := range typed {
			clone[key] = allowTemplateExpressionsInSchema(value)
		}
		if schemaNeedsTemplateFallback(clone) {
			return map[string]any{
				"anyOf": []any{
					clone,
					map[string]any{
						"type":    "string",
						"pattern": `^\s*\{\{[\s\S]*\}\}\s*$`,
					},
				},
			}
		}
		return clone
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = allowTemplateExpressionsInSchema(value)
		}
		return out
	default:
		return typed
	}
}

func schemaNeedsTemplateFallback(schema map[string]any) bool {
	if _, ok := schema["$id"]; ok {
		return false
	}
	if _, ok := schema["enum"]; ok {
		return true
	}
	if _, ok := schema["const"]; ok {
		return true
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf", "not"} {
		if _, ok := schema[key]; ok {
			return true
		}
	}

	rawType, ok := schema["type"]
	if !ok {
		return false
	}

	switch typed := rawType.(type) {
	case string:
		return typed != "string"
	case []any:
		for _, value := range typed {
			s, ok := value.(string)
			if ok && s == "string" {
				return false
			}
		}
		return true
	default:
		return false
	}
}
