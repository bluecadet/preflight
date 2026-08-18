package schemavalidation

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"gopkg.in/yaml.v3"

	schemafiles "github.com/bluecadet/preflight/schema"
)

// templatePatternSource is the regex (as JSON Schema "pattern") used to
// recognize a bare "{{ ... }}" template expression standing in for a
// non-string-typed value. It is the single source of truth for both
// constructing the template escape-hatch branch (allowTemplateExpressionsInSchema)
// and recognizing that branch again in a validation error tree
// (isTemplatePatternBranch), so the two can never drift apart.
const templatePatternSource = `^\s*\{\{[\s\S]*\}\}\s*$`

// resource pairs a schema's canonical URL with its embedded file path.
type resource struct {
	URL  string
	Path string
}

var (
	schemaOnce     sync.Once
	schemaCache    map[string]*jsonschema.Schema
	schemaCacheErr error

	// templateBranches holds the compiled Schema.Location of every synthetic
	// "{{ ... }}" escape-hatch branch injected by allowTemplateExpressionsInSchema,
	// so that simplifyValidationError can recognize and discard failures from
	// that branch instead of surfacing them as if they were a real schema rule.
	templateBranches map[string]struct{}

	// allResources is the single point where a schema must be registered to
	// be resolvable by ValidateYAML/ValidateDocument, including as the target
	// of a cross-schema $ref (e.g. config.schema.json's "inventory" property
	// refs into inventory.schema.json#/$defs/inventory). Callers cannot
	// influence which schemas get compiled; adding a new schemaURL requires
	// adding it here.
	allResources = []resource{
		{URL: "https://preflight.dev/schema/action.schema.json", Path: "action.schema.json"},
		{URL: "https://preflight.dev/schema/playbook.schema.json", Path: "playbook.schema.json"},
		{URL: "https://preflight.dev/schema/inventory.schema.json", Path: "inventory.schema.json"},
		{URL: "https://preflight.dev/schema/config.schema.json", Path: "config.schema.json"},
	}
)

func ValidateYAML(data []byte, schemaURL string) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return ValidateDocument(map[string]any{}, schemaURL)
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("schema validation parse error: %w", err)
	}

	return ValidateDocument(doc, schemaURL)
}

func ValidateDocument(doc any, schemaURL string) error {
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
		verr, ok := err.(*jsonschema.ValidationError)
		if !ok {
			// Not expected from this library, but fall back to the raw
			// error rather than hide it.
			return fmt.Errorf("schema validation failed: %w", err)
		}
		simplified := simplifyValidationError(verr, templateBranches)
		return fmt.Errorf("schema validation failed: %w", &actionableValidationError{
			text:  formatValidationError(simplified),
			cause: simplified,
		})
	}
	return nil
}

// actionableValidationError carries a rewritten, human-actionable message
// alongside the (also rewritten) underlying *jsonschema.ValidationError, so
// that callers doing errors.As(&err, &verr) (see
// TestValidateYAML_UnderlyingValidationErrorAccessibleViaErrorsAs) still get
// a *jsonschema.ValidationError, while ValidateYAML/ValidateDocument's own
// Error() text is ours to control rather than the library's default
// "jsonschema validation failed with '<url>'" plus raw anyOf/oneOf tree dump.
type actionableValidationError struct {
	text  string
	cause *jsonschema.ValidationError
}

func (e *actionableValidationError) Error() string { return e.text }
func (e *actionableValidationError) Unwrap() error { return e.cause }

// simplifyValidationError rewrites a validation error tree so that failures
// coming purely from the synthetic "{{ ... }}" template escape-hatch branch
// (see allowTemplateExpressionsInSchema) are discarded rather than reported
// as if they were a real, independent schema violation.
//
// Design decision: allowTemplateExpressionsInSchema wraps every non-string
// schema node in `anyOf: [originalSchema, templateStringPattern]`. When a
// value is neither the real type nor a template string, jsonschema reports
// the anyOf as failed with exactly two causes, in declared order: causes[0]
// is always the original schema's own failure, causes[1] is always the
// template branch's failure. Rather than reporting both alternatives (which
// would read as "expected X, or expected a {{ }} template" for every single
// type/enum/required/additionalProperties mismatch in the document), this
// keeps only causes[0] -- the original schema's honest complaint -- and
// discards causes[1] entirely. Template-expression support is a documented,
// blanket contract of every schema in this package (see this package's
// AGENTS.md), not a per-field detail worth repeating in every error message;
// a user who is not writing a template does not need to be told one was a
// theoretical alternative.
//
// Because allowTemplateExpressionsInSchema wraps at every nesting level (not
// just at the leaf with the real problem), a single deeply nested mismatch
// otherwise produces one such anyOf wrapper failure per ancestor level on
// the path to the root. Discarding causes[1] and splicing causes[0] in place
// of the anyOf node collapses all of those synthetic wrappers, at every
// level, down to the one real failure underneath.
//
// A genuine, hand-authored anyOf/oneOf/allOf in the schema itself (e.g.
// registryBinaryPatchSpec.data's `oneOf: [{integer}, {string}]`) is left
// completely untouched: it is not a *kind.AnyOf with exactly two causes
// where the second is recognized (via templateBranches, an exact,
// non-heuristic identity check -- see collectTemplateBranches) as our own
// injected branch.
func simplifyValidationError(verr *jsonschema.ValidationError, templateBranches map[string]struct{}) *jsonschema.ValidationError {
	if verr == nil {
		return nil
	}

	if _, isAnyOf := verr.ErrorKind.(*kind.AnyOf); isAnyOf && len(verr.Causes) == 2 {
		if _, isTemplateBranch := templateBranches[verr.Causes[1].SchemaURL]; isTemplateBranch {
			return simplifyValidationError(verr.Causes[0], templateBranches)
		}
	}

	if len(verr.Causes) == 0 {
		return verr
	}

	clone := *verr
	clone.Causes = make([]*jsonschema.ValidationError, len(verr.Causes))
	for i, cause := range verr.Causes {
		clone.Causes[i] = simplifyValidationError(cause, templateBranches)
	}
	return &clone
}

// formatValidationError renders a (simplified) validation error tree as a
// flat list of actionable, self-contained lines -- one per real leaf
// problem, each naming its own JSON-pointer instance location, in the same
// "at '/path': reason" wording the underlying library already uses (see
// TestValidateYAML_ErrorsIdentifyFieldPath). Every simultaneous problem is
// reported, matching the current (pre-existing) "report all errors, not
// just the first" contract -- see TestValidateYAML_AllErrorsReportedNotJustFirst.
func formatValidationError(verr *jsonschema.ValidationError) string {
	messages := flattenValidationMessages(verr, nil)
	switch len(messages) {
	case 0:
		return verr.Error()
	case 1:
		return messages[0]
	default:
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d schema violations:", len(messages))
		for _, msg := range messages {
			sb.WriteString("\n  ")
			sb.WriteString(msg)
		}
		return sb.String()
	}
}

// flattenValidationMessages recurses through a validation error tree,
// appending one line per node whose ErrorKind carries real information.
// *kind.Schema (the root wrapper) and *kind.Group (the "N simultaneous
// errors at this node" wrapper) and *kind.Reference (the "$ref failed"
// wrapper) never carry a message worth surfacing on their own -- their
// LocalizedString is always a generic placeholder ("jsonschema validation
// failed with ...", "validation failed") -- so those are skipped and only
// their causes are recursed into. Every other kind (Type, Required, Enum,
// AdditionalProperties, a genuine AnyOf/OneOf/AllOf, ...) gets its own line,
// via the library's own per-node Error() rendering (a shallow copy with
// Causes cleared, so nested causes aren't double-rendered), in addition to
// recursing into any causes it has.
//
// kind.Enum and kind.Const are a deliberate exception: their own
// LocalizedString lists only the allowed values ("value must be one of
// ..."), never the offending value itself -- that omission was previously
// papered over by accident, because the synthetic template-tolerance branch
// (see simplifyValidationError) happened to echo the raw value in its own
// (now-discarded) pattern-mismatch message. We restore it explicitly here
// instead of relying on that side effect.
func flattenValidationMessages(verr *jsonschema.ValidationError, messages []string) []string {
	if verr == nil {
		return messages
	}

	switch k := verr.ErrorKind.(type) {
	case *kind.Group, *kind.Schema, *kind.Reference:
		// Transparent wrappers: no message of their own worth surfacing.
	case *kind.Enum:
		messages = append(messages, appendGotValue(verr, k.Got))
	case *kind.Const:
		messages = append(messages, appendGotValue(verr, k.Got))
	default:
		leaf := *verr
		leaf.Causes = nil
		messages = append(messages, leaf.Error())
	}

	for _, cause := range verr.Causes {
		messages = flattenValidationMessages(cause, messages)
	}
	return messages
}

// appendGotValue renders verr the normal way (a shallow, causes-cleared
// copy through the library's own Error()) and appends the actual offending
// value, quoted in the same style the library uses for its own "value must
// be one of ..." lists (see displayValue).
func appendGotValue(verr *jsonschema.ValidationError, got any) string {
	leaf := *verr
	leaf.Causes = nil
	return fmt.Sprintf("%s (got %s)", leaf.Error(), displayValue(got))
}

// displayValue mirrors jsonschema/v6/kind's own unexported "display"
// helper: primitive scalars render with %v, strings are single-quoted (with
// embedded quotes escaped) to match the quoting used throughout this
// library's own error text, and composite values (which could be
// arbitrarily large) are elided to "value".
func displayValue(v any) string {
	switch v := v.(type) {
	case string:
		q := fmt.Sprintf("%q", v)
		q = strings.ReplaceAll(q, `\"`, `"`)
		q = strings.ReplaceAll(q, `'`, `\'`)
		return "'" + q[1:len(q)-1] + "'"
	case []any, map[string]any:
		return "value"
	default:
		return fmt.Sprintf("%v", v)
	}
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

		templateBranches = make(map[string]struct{})
		visited := make(map[*jsonschema.Schema]bool)
		for _, resource := range allResources {
			collectTemplateBranches(schemaCache[resource.URL], visited, templateBranches)
		}
	})

	return schemaCache, schemaCacheErr
}

// collectTemplateBranches walks a compiled schema graph looking for nodes
// that are structurally identical to the synthetic escape-hatch branch built
// by allowTemplateExpressionsInSchema (a bare `{"type":"string","pattern":
// templatePatternSource}`), and records their compiled Location. Compiled
// Schema.Location is authoritative (it is the library's own resolved
// URL+JSON-pointer for that node), so this gives simplifyValidationError an
// exact, non-heuristic way to recognize "this failure came from the
// template branch we injected" without guessing from message text.
//
// Schemas form a graph, not a tree, once $ref/$defs are compiled (multiple
// locations can point at the same *Schema, and cross-schema refs such as
// config.schema.json -> inventory.schema.json introduce sharing across
// resources) hence the shared visited set across all resources.
func collectTemplateBranches(sch *jsonschema.Schema, visited map[*jsonschema.Schema]bool, out map[string]struct{}) {
	if sch == nil || visited[sch] {
		return
	}
	visited[sch] = true

	if isTemplatePatternBranch(sch) {
		out[sch.Location] = struct{}{}
	}

	collectTemplateBranches(sch.Not, visited, out)
	for _, s := range sch.AllOf {
		collectTemplateBranches(s, visited, out)
	}
	for _, s := range sch.AnyOf {
		collectTemplateBranches(s, visited, out)
	}
	for _, s := range sch.OneOf {
		collectTemplateBranches(s, visited, out)
	}
	collectTemplateBranches(sch.If, visited, out)
	collectTemplateBranches(sch.Then, visited, out)
	collectTemplateBranches(sch.Else, visited, out)
	collectTemplateBranches(sch.PropertyNames, visited, out)
	for _, s := range sch.Properties {
		collectTemplateBranches(s, visited, out)
	}
	for _, s := range sch.PatternProperties {
		collectTemplateBranches(s, visited, out)
	}
	if s, ok := sch.AdditionalProperties.(*jsonschema.Schema); ok {
		collectTemplateBranches(s, visited, out)
	}
	for _, s := range sch.DependentSchemas {
		collectTemplateBranches(s, visited, out)
	}
	collectTemplateBranches(sch.UnevaluatedProperties, visited, out)
	collectTemplateBranches(sch.Contains, visited, out)
	switch items := sch.Items.(type) {
	case *jsonschema.Schema:
		collectTemplateBranches(items, visited, out)
	case []*jsonschema.Schema:
		for _, s := range items {
			collectTemplateBranches(s, visited, out)
		}
	}
	if s, ok := sch.AdditionalItems.(*jsonschema.Schema); ok {
		collectTemplateBranches(s, visited, out)
	}
	for _, s := range sch.PrefixItems {
		collectTemplateBranches(s, visited, out)
	}
	collectTemplateBranches(sch.Items2020, visited, out)
	collectTemplateBranches(sch.UnevaluatedItems, visited, out)
	collectTemplateBranches(sch.ContentSchema, visited, out)
	collectTemplateBranches(sch.Ref, visited, out)
	collectTemplateBranches(sch.RecursiveRef, visited, out)
}

// isTemplatePatternBranch reports whether sch is exactly the synthetic
// escape-hatch schema allowTemplateExpressionsInSchema injects: a leaf
// requiring type "string" matching templatePatternSource and nothing else.
func isTemplatePatternBranch(sch *jsonschema.Schema) bool {
	if sch.Pattern == nil || sch.Pattern.String() != templatePatternSource {
		return false
	}
	if sch.Types == nil {
		return false
	}
	types := sch.Types.ToStrings()
	return len(types) == 1 && types[0] == "string"
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
						"pattern": templatePatternSource,
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
