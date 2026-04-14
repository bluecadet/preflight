package schemavalidation

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	schemafiles "github.com/bluecadet/preflight/schema"
)

type Resource struct {
	URL  string
	Path string
}

var (
	schemaOnce     sync.Once
	schemaCache    map[string]*jsonschema.Schema
	schemaCacheErr error
	allResources   = []Resource{
		{URL: "https://preflight.dev/schema/action.schema.json", Path: "action.schema.json"},
		{URL: "https://preflight.dev/schema/playbook.schema.json", Path: "playbook.schema.json"},
		{URL: "https://preflight.dev/schema/inventory.schema.json", Path: "inventory.schema.json"},
		{URL: "https://preflight.dev/schema/config.schema.json", Path: "config.schema.json"},
	}
)

func ValidateYAML(data []byte, schemaURL string, resources []Resource) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return ValidateDocument(map[string]any{}, schemaURL, resources)
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("schema validation parse error: %w", err)
	}

	return ValidateDocument(doc, schemaURL, resources)
}

func ValidateDocument(doc any, schemaURL string, resources []Resource) error {
	normalized, err := normalizeDocument(doc)
	if err != nil {
		return fmt.Errorf("schema validation parse error: %w", err)
	}

	schemas, err := compiledSchemas()
	if err != nil {
		return fmt.Errorf("schema validation setup error: %w", err)
	}

	schema, ok := schemas[schemaURL]
	if !ok {
		return fmt.Errorf("schema validation setup error: missing compiled schema for %q", schemaURL)
	}

	if err := schema.Validate(normalized); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

func compiledSchemas() (map[string]*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)

		schemaCache = make(map[string]*jsonschema.Schema, len(allResources))
		for _, resource := range allResources {
			data, err := schemafiles.FS.ReadFile(resource.Path)
			if err != nil {
				schemaCacheErr = err
				return
			}

			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if err != nil {
				schemaCacheErr = fmt.Errorf("load %s: %w", resource.Path, err)
				return
			}
			if err := compiler.AddResource(resource.URL, allowTemplateExpressionsInSchema(doc)); err != nil {
				schemaCacheErr = fmt.Errorf("add %s: %w", resource.Path, err)
				return
			}
		}

		for _, resource := range allResources {
			schemaCache[resource.URL], schemaCacheErr = compiler.Compile(resource.URL)
			if schemaCacheErr != nil {
				schemaCacheErr = fmt.Errorf("compile %s: %w", resource.Path, schemaCacheErr)
				return
			}
		}
	})

	return schemaCache, schemaCacheErr
}

func normalizeDocument(doc any) (any, error) {
	switch typed := doc.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return typed, nil
	case map[string]any, map[any]any, []any:
		return normalizeYAMLValue(typed)
	default:
		data, err := yaml.Marshal(doc)
		if err != nil {
			return nil, err
		}

		var decoded any
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			return nil, err
		}
		return normalizeYAMLValue(decoded)
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
