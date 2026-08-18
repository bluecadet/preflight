package schemavalidation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bluecadet/preflight/internal/schemavalidation"
)

// Schema URLs and per-caller resource lists, mirroring the values each real
// call site (internal/action, internal/config, internal/inventory) passes to
// schemavalidation.ValidateYAML. See TestValidateYAML_ResourcesParameterHasNoEffect
// for why these per-caller lists turn out not to matter.
const (
	actionSchemaURL    = "https://preflight.dev/schema/action.schema.json"
	playbookSchemaURL  = "https://preflight.dev/schema/playbook.schema.json"
	configSchemaURL    = "https://preflight.dev/schema/config.schema.json"
	inventorySchemaURL = "https://preflight.dev/schema/inventory.schema.json"
)

var (
	actionResources = []schemavalidation.Resource{
		{URL: actionSchemaURL, Path: "action.schema.json"},
		{URL: playbookSchemaURL, Path: "playbook.schema.json"},
	}
	playbookResources = actionResources
	configResources   = []schemavalidation.Resource{
		{URL: configSchemaURL, Path: "config.schema.json"},
	}
	inventoryResources = []schemavalidation.Resource{
		{URL: inventorySchemaURL, Path: "inventory.schema.json"},
	}
)

// ---------------------------------------------------------------------------
// Valid documents, adapted from real fixtures in the repo (demo/, and the
// embedded preflight/windows-power stdlib action) rather than invented ones.
// ---------------------------------------------------------------------------

// validAction is trimmed from internal/stdlib/actions/preflight/windows-power/action.yml.
const validAction = `
name: preflight/windows-power
version: "3.1.0"
description: Configure Windows power plans and screen saver settings
author: preflight

inputs:
  user:
    type: string
    default: ""
  plan_name:
    type: string
    default: Preflight Exhibit
  activate_plan:
    type: bool
    default: true

tasks:
  - name: Configure power plan
    power_plan:
      name: "{{ vars.plan_name }}"
      base: balanced
      activate: "{{ vars.activate_plan }}"
      settings:
        - subgroup: SUB_VIDEO
          setting: VIDEOIDLE
          ac_value: "{{ vars.display_timeout_ac }}"
          dc_value: 0
      ensure: present

  - name: Configure screen saver
    powershell:
      env:
        PREFLIGHT_USER: "{{ vars.user }}"
      check_script: |
        exit 0
      script: |
        exit 0
`

// validPlaybook is demo/playbooks/baseline.yml verbatim.
const validPlaybook = `
name: baseline-test-pc
description: >
  Idempotent check/apply playbook. Each task uses check_script + script so the
  TUI shows the "would change" diff on the first run and status=ok on the
  second. Run it twice to see Preflight converge.

vars:
  staging_dir: "C:\\ProgramData\\PreflightDemo"
  reclaim: false

tasks:
  - name: Ensure staging directory exists
    directory:
      path: "{{ vars.staging_dir }}"
      ensure: present

  - name: Write greeting file
    powershell:
      check_script: |
        exit 0
      script: |
        exit 0

  - name: Set machine-level env var
    environment:
      name: PREFLIGHT_DEMO
      value: "configured"
      scope: machine

  - name: Reclaim the greeting file (cleanup toggle)
    when: "{{ vars.reclaim }}"
    ignore_errors: true
    powershell:
      check_script: |
        exit 0
      script: |
        exit 0
`

// validConfig is adapted from demo/preflight.yml, including its embedded inventory.
const validConfig = `
project: preflight-demo
environment: test

vars:
  owner: pf-test

secrets:
  identity: keys/identity.age
  recipients:
    - age1example
  entries:
    db_password:
      file: secrets/db-password.age
      type: age

inventory:
  vars:
    site: test-lab

  groups:
    windows:
      vars:
        os_family: windows

  hosts:
    - name: test-pc
      address: 192.168.207.131
      transport: ssh
      port: 22
      username: pf-test
      password: password
      host_key_policy: insecure
      timeout: 30s
      groups: [windows]
      vars:
        display: primary
`

// validInventory mirrors the sample used in internal/inventory's own tests.
const validInventory = `
vars:
  timezone: "America/New_York"
groups:
  lobby:
    vars:
      resolution: "3840x2160"
hosts:
  - name: lobby-pc-01
    address: 192.168.1.10
    transport: winrm
    groups: [lobby]
  - name: local-ts
    transport: local
    platform:
      os: windows
      arch: amd64
`

func TestValidateYAML_ValidDocumentsPass(t *testing.T) {
	tests := map[string]struct {
		data      string
		schemaURL string
		resources []schemavalidation.Resource
	}{
		"action":    {validAction, actionSchemaURL, actionResources},
		"playbook":  {validPlaybook, playbookSchemaURL, playbookResources},
		"config":    {validConfig, configSchemaURL, configResources},
		"inventory": {validInventory, inventorySchemaURL, inventoryResources},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := schemavalidation.ValidateYAML([]byte(tt.data), tt.schemaURL, tt.resources); err != nil {
				t.Fatalf("expected valid %s document to pass, got: %v", name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Invalid documents: wrong types, missing required fields.
// ---------------------------------------------------------------------------

func TestValidateYAML_MissingRequiredFieldsRejected(t *testing.T) {
	tests := map[string]struct {
		data      string
		schemaURL string
		resources []schemavalidation.Resource
		wantSub   string // substring expected somewhere in the error
	}{
		"action missing name": {
			data: `
tasks:
  - name: t1
    shell:
      cmd: echo hi
`,
			schemaURL: actionSchemaURL,
			resources: actionResources,
			wantSub:   "'name'",
		},
		"action missing tasks": {
			data: `
name: myorg/action
`,
			schemaURL: actionSchemaURL,
			resources: actionResources,
			wantSub:   "'tasks'",
		},
		"task missing name": {
			data: `
name: p
tasks:
  - shell:
      cmd: echo hi
`,
			schemaURL: playbookSchemaURL,
			resources: playbookResources,
			wantSub:   "'name'",
		},
		"inventory host missing name": {
			data: `
hosts:
  - address: 10.0.0.1
`,
			schemaURL: inventorySchemaURL,
			resources: inventoryResources,
			wantSub:   "'name'",
		},
		"platform missing arch": {
			data: `
hosts:
  - name: ts1
    platform:
      os: windows
`,
			schemaURL: inventorySchemaURL,
			resources: inventoryResources,
			wantSub:   "'arch'",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := schemavalidation.ValidateYAML([]byte(tt.data), tt.schemaURL, tt.resources)
			if err == nil {
				t.Fatal("expected schema validation error, got nil")
			}
			if !strings.Contains(err.Error(), "schema validation failed") {
				t.Fatalf("error = %q, want a schema validation failure", err)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestValidateYAML_WrongTypesRejected(t *testing.T) {
	tests := map[string]struct {
		data      string
		schemaURL string
		resources []schemavalidation.Resource
	}{
		"tasks not an array": {
			data:      "name: p\ntasks: 5\n",
			schemaURL: playbookSchemaURL,
			resources: playbookResources,
		},
		"vars not an object": {
			data:      "vars: []\n",
			schemaURL: configSchemaURL,
			resources: configResources,
		},
		"hosts not an array": {
			data:      "hosts: {}\n",
			schemaURL: inventorySchemaURL,
			resources: inventoryResources,
		},
		"whole document a scalar": {
			data:      "just a plain string\n",
			schemaURL: playbookSchemaURL,
			resources: playbookResources,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := schemavalidation.ValidateYAML([]byte(tt.data), tt.schemaURL, tt.resources)
			if err == nil {
				t.Fatal("expected schema validation error, got nil")
			}
			if !strings.Contains(err.Error(), "schema validation failed") {
				t.Fatalf("error = %q, want a schema validation failure", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// additionalProperties enforcement: typo'd/unknown keys must be rejected,
// not silently ignored. This is the check the task description calls out
// specifically, since a schema that permits unknown fields would let typo'd
// keys pass through silently.
// ---------------------------------------------------------------------------

func TestValidateYAML_UnknownFieldsRejected(t *testing.T) {
	tests := map[string]struct {
		data      string
		schemaURL string
		resources []schemavalidation.Resource
		wantField string
	}{
		"unknown top-level config field": {
			data:      "projct: typo\n", // typo of "project"
			schemaURL: configSchemaURL,
			resources: configResources,
			wantField: "projct",
		},
		"unknown top-level playbook field": {
			data:      "nam: typo\ntasks: []\n", // typo of "name"
			schemaURL: playbookSchemaURL,
			resources: playbookResources,
			wantField: "nam",
		},
		"unknown field inside a module block": {
			data: `
name: p
tasks:
  - name: t1
    shell:
      cmd: echo hi
      bogus_option: true
`,
			schemaURL: playbookSchemaURL,
			resources: playbookResources,
			wantField: "bogus_option",
		},
		"unknown field inside inventory host": {
			data: `
hosts:
  - name: h1
    hostname: h1.example.com
`, // "hostname" is not a recognized key; the schema uses "address"
			schemaURL: inventorySchemaURL,
			resources: inventoryResources,
			wantField: "hostname",
		},
		"unknown field inside action inputDef": {
			data: `
name: a
tasks: []
inputs:
  foo:
    type: string
    fallback_value: bar
`, // not a recognized inputDef key; the schema uses "default"
			schemaURL: actionSchemaURL,
			resources: actionResources,
			wantField: "fallback_value",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := schemavalidation.ValidateYAML([]byte(tt.data), tt.schemaURL, tt.resources)
			if err == nil {
				t.Fatalf("expected %q to be rejected as an unknown/additional property, but it validated", tt.wantField)
			}
			if !strings.Contains(err.Error(), "schema validation failed") {
				t.Fatalf("error = %q, want a schema validation failure", err)
			}
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Fatalf("error = %q, want it to name the offending field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateYAML_EnumValuesEnforced(t *testing.T) {
	tests := map[string]struct {
		data    string
		wantSub string
	}{
		"invalid service state": {
			data: `
name: p
tasks:
  - name: t1
    service:
      state: sideways
`,
			wantSub: "value must be one of 'running', 'stopped', 'disabled'",
		},
		"invalid registry value type": {
			data: `
name: p
tasks:
  - name: t1
    registry:
      path: HKLM:\SOFTWARE\x
      values:
        - name: v1
          type: notarealtype
`,
			wantSub: "value must be one of 'string', 'expand_string', 'dword', 'qword', 'binary', 'multi_string'",
		},
		"invalid inventory transport": {
			data: `
hosts:
  - name: h1
    transport: telnet
`,
			wantSub: "value must be one of 'winrm', 'ssh', 'local'",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			url := playbookSchemaURL
			resources := playbookResources
			if strings.Contains(name, "inventory") {
				url = inventorySchemaURL
				resources = inventoryResources
			}
			err := schemavalidation.ValidateYAML([]byte(tt.data), url, resources)
			if err == nil {
				t.Fatal("expected schema validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantSub)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error message quality: errors must identify the offending field path in a
// way a user can act on. The underlying library reports JSON Pointer paths
// via "at '/...'" segments; assert those paths are present and correct for
// both shallow and deeply nested failures.
// ---------------------------------------------------------------------------

func TestValidateYAML_ErrorsIdentifyFieldPath(t *testing.T) {
	tests := map[string]struct {
		data     string
		wantPath string
	}{
		"top-level field": {
			data:     "projct: x\n",
			wantPath: "at ''",
		},
		"task-level field": {
			data: `
name: p
tasks:
  - name: t1
    service:
      state: sideways
`,
			wantPath: "/tasks/0/service/state",
		},
		"deeply nested registry patch field": {
			data: `
name: p
tasks:
  - name: t1
    registry:
      path: HKLM:\SOFTWARE\x
      values:
        - name: v1
          patch:
            - offset: -1
              data: 1
`,
			wantPath: "/tasks/0/registry/values/0/patch/0/offset",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := schemavalidation.ValidateYAML([]byte(tt.data), playbookSchemaURL, playbookResources)
			if err == nil {
				t.Fatal("expected schema validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("error = %q, want it to reference path %q", err, tt.wantPath)
			}
		})
	}
}

// TestValidateYAML_AllErrorsReportedNotJustFirst answers one of the specific
// questions in the task: when a document has multiple independent problems,
// the jsonschema library collects and reports all of them (nested under the
// enclosing anyOf/array failures), not merely the first one encountered.
func TestValidateYAML_AllErrorsReportedNotJustFirst(t *testing.T) {
	data := `
name: p
tasks:
  - name: t1
    service:
      state: bogus
  - name: t2
    reboot:
      timeout: not-a-number
`
	err := schemavalidation.ValidateYAML([]byte(data), playbookSchemaURL, playbookResources)
	if err == nil {
		t.Fatal("expected schema validation error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "/tasks/0/service/state") {
		t.Errorf("error missing first task's failure location: %q", msg)
	}
	if !strings.Contains(msg, "/tasks/1/reboot/timeout") {
		t.Errorf("error missing second task's failure location: %q", msg)
	}
}

// ---------------------------------------------------------------------------
// Malformed YAML vs. schema-invalid YAML must be distinguishable so callers
// can react differently (e.g. a syntax error vs. a contract violation).
// ---------------------------------------------------------------------------

func TestValidateYAML_MalformedYAMLDistinctFromSchemaViolation(t *testing.T) {
	t.Run("malformed YAML", func(t *testing.T) {
		err := schemavalidation.ValidateYAML([]byte("{{{"), playbookSchemaURL, playbookResources)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "schema validation parse error") {
			t.Fatalf("error = %q, want a parse error", err)
		}
		if strings.Contains(err.Error(), "schema validation failed") {
			t.Fatalf("error = %q, malformed YAML should not be reported as a schema violation", err)
		}
	})

	t.Run("well-formed YAML violating the schema", func(t *testing.T) {
		err := schemavalidation.ValidateYAML([]byte("tasks: 5\n"), playbookSchemaURL, playbookResources)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "schema validation failed") {
			t.Fatalf("error = %q, want a schema validation failure", err)
		}
		if strings.Contains(err.Error(), "schema validation parse error") {
			t.Fatalf("error = %q, a schema violation should not be reported as a parse error", err)
		}
	})
}

// TestValidateYAML_NonStringMapKeyIsAParseError documents a corner of YAML
// that a document author could hit by accident: an unquoted "true"/"false"
// (or a bare integer) as a mapping key decodes to a non-string Go key, which
// this package treats as a parse-time failure rather than a schema failure.
//
// NOTE: unlike every other parse/validation error in this package, this
// particular error carries no location information (no field name, no JSON
// pointer) -- see the "weak error messages" finding in the review notes.
func TestValidateYAML_NonStringMapKeyIsAParseError(t *testing.T) {
	err := schemavalidation.ValidateYAML([]byte("name: p\nvars:\n  true: foo\ntasks: []\n"), playbookSchemaURL, playbookResources)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "schema validation parse error") {
		t.Fatalf("error = %q, want a parse error", err)
	}
	if strings.Contains(err.Error(), "schema validation failed") {
		t.Fatalf("error = %q, should not be reported as a schema violation", err)
	}
}

// ---------------------------------------------------------------------------
// Empty document handling.
// ---------------------------------------------------------------------------

func TestValidateYAML_EmptyDocument(t *testing.T) {
	tests := map[string]struct {
		data      []byte
		schemaURL string
		resources []schemavalidation.Resource
		wantErr   bool
	}{
		"nil bytes against playbook (no required fields)":  {nil, playbookSchemaURL, playbookResources, false},
		"empty bytes against playbook":                     {[]byte(""), playbookSchemaURL, playbookResources, false},
		"whitespace-only against playbook":                 {[]byte("   \n\t\n"), playbookSchemaURL, playbookResources, false},
		"nil bytes against config (no required fields)":    {nil, configSchemaURL, configResources, false},
		"empty bytes against action (name+tasks required)": {[]byte(""), actionSchemaURL, actionResources, true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := schemavalidation.ValidateYAML(tt.data, tt.schemaURL, tt.resources)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Template-expression tolerance: fields typed as enum/int/bool/object must
// also accept a bare "{{ ... }}" template expression string, per the
// package's documented "Common Patterns" contract, while still rejecting
// non-template garbage strings in those same positions.
// ---------------------------------------------------------------------------

func TestValidateYAML_TemplateExpressionsTolerated(t *testing.T) {
	valid := map[string]string{
		"templated enum (service.state)": `
name: p
tasks:
  - name: t1
    service:
      state: "{{ vars.desired_state }}"
`,
		"templated integer (reboot.timeout)": `
name: p
tasks:
  - name: t1
    reboot:
      timeout: "{{ vars.timeout_seconds }}"
`,
		"templated boolean (ignore_errors is native bool, task-level uses string when)": `
name: p
tasks:
  - name: t1
    when: "{{ vars.enabled }}"
    shell:
      cmd: echo hi
`,
		"templated registry value type": `
name: p
tasks:
  - name: t1
    registry:
      path: HKLM:\SOFTWARE\x
      values:
        - name: v1
          type: "{{ vars.regtype }}"
`,
	}
	for name, data := range valid {
		t.Run(name, func(t *testing.T) {
			if err := schemavalidation.ValidateYAML([]byte(data), playbookSchemaURL, playbookResources); err != nil {
				t.Fatalf("expected template expression to validate, got: %v", err)
			}
		})
	}

	t.Run("near-template garbage is still rejected", func(t *testing.T) {
		// Missing the closing braces -- must not be treated as a template escape hatch.
		data := `
name: p
tasks:
  - name: t1
    service:
      state: "{{ vars.desired_state"
`
		err := schemavalidation.ValidateYAML([]byte(data), playbookSchemaURL, playbookResources)
		if err == nil {
			t.Fatal("expected schema validation error for malformed template-like string, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateDocument: exercise the non-YAML entry point directly, including
// normalizeDocument's map[any]any path (which yaml.Unmarshal-into-`any` can
// itself produce for non-string YAML keys -- see TestValidateYAML_NonStringMapKeyIsAParseError
// for the ValidateYAML-level view of the same underlying code path).
// ---------------------------------------------------------------------------

func TestValidateDocument_NonStringKeyRejected(t *testing.T) {
	doc := map[any]any{
		"name": "p",
		"vars": map[any]any{
			1: "bad key",
		},
		"tasks": []any{},
	}
	err := schemavalidation.ValidateDocument(doc, playbookSchemaURL, playbookResources)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "schema validation parse error") {
		t.Fatalf("error = %q, want a parse error", err)
	}
}

func TestValidateDocument_MapAnyAnyWithStringKeysNormalizes(t *testing.T) {
	doc := map[any]any{
		"name": "p",
		"tasks": []any{
			map[any]any{
				"name": "t1",
				"shell": map[any]any{
					"cmd": "echo hi",
				},
			},
		},
	}
	if err := schemavalidation.ValidateDocument(doc, playbookSchemaURL, playbookResources); err != nil {
		t.Fatalf("expected map[any]any document with string keys to validate, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Regression guard for a subtle naming collision: several schemas define a
// property literally named "type" (registryValueSpec.type, inputDef.type).
// The template-tolerance rewrite (allowTemplateExpressionsInSchema) walks
// every map in the schema document generically and keys off of a literal
// "type" key to decide whether to inject template tolerance -- so a
// "properties" object that happens to contain a property named "type" could,
// in principle, be misidentified as a schema fragment itself. This test
// pins down that the collision is currently harmless (the value under that
// key is a schema object, not a string/[]any, so the type-switch in
// schemaNeedsTemplateFallback falls through to its default case) by
// asserting both the enum check and template tolerance still work correctly
// on those exact fields.
// ---------------------------------------------------------------------------

func TestValidateYAML_TypePropertyNameCollisionIsHarmless(t *testing.T) {
	t.Run("registryValueSpec.type enum enforced", func(t *testing.T) {
		data := `
name: p
tasks:
  - name: t1
    registry:
      path: HKLM:\SOFTWARE\x
      values:
        - name: v1
          type: not-a-real-type
`
		err := schemavalidation.ValidateYAML([]byte(data), playbookSchemaURL, playbookResources)
		if err == nil {
			t.Fatal("expected invalid registry value type to be rejected")
		}
	})

	t.Run("inputDef.type enum enforced", func(t *testing.T) {
		data := `
name: a
tasks: []
inputs:
  foo:
    type: not-a-real-type
`
		err := schemavalidation.ValidateYAML([]byte(data), actionSchemaURL, actionResources)
		if err == nil {
			t.Fatal("expected invalid input type to be rejected")
		}
	})
}

// ---------------------------------------------------------------------------
// KNOWN BUG: the `resources []Resource` parameter accepted by ValidateYAML
// and ValidateDocument has no effect on validation. compiledSchemas() always
// compiles the package-level `allResources` var (all four schemas), ignoring
// whatever the caller passed in. Every real call site (internal/action,
// internal/config, internal/inventory) builds its own narrower resources
// slice and passes it through, but that work is discarded.
//
// This is currently harmless only because `allResources` happens to already
// contain every schema needed to resolve every cross-schema $ref (config.yml
// embeds an inventory block via $ref into inventory.schema.json). If a new
// schema were ever added without also updating the internal `allResources`
// list, no caller-supplied `resources` argument -- however complete -- could
// fix it, because the argument is never consulted.
//
// These tests are characterization tests: they document current behavior,
// not a guarantee. They should be revisited (and likely simplified, e.g. by
// dropping the parameter and using allResources directly, or by actually
// threading it through compiledSchemas) if this is fixed.
// ---------------------------------------------------------------------------

func TestValidateYAML_ResourcesParameterHasNoEffect(t *testing.T) {
	// config.schema.json's "inventory" property $refs into
	// inventory.schema.json#/$defs/inventory. The config package's own
	// resources list (mirrored in configResources above) does NOT include
	// the inventory schema. If the `resources` argument were actually used
	// to drive schema compilation, resolving that $ref would fail. It
	// doesn't, because compiledSchemas() ignores the argument and always
	// compiles the full package-level allResources set instead.
	data := []byte(`
project: p
inventory:
  hosts:
    - name: h1
`)

	if err := schemavalidation.ValidateYAML(data, configSchemaURL, configResources); err != nil {
		t.Fatalf("config's own (incomplete) resources list should validate the same as any other list, got: %v", err)
	}

	// Passing nil, or a completely fabricated/irrelevant list, produces an
	// identical result -- proof the argument is inert.
	if err := schemavalidation.ValidateYAML(data, configSchemaURL, nil); err != nil {
		t.Fatalf("nil resources should behave identically to configResources, got: %v", err)
	}
	bogus := []schemavalidation.Resource{{URL: "https://example.invalid/nope.json", Path: "does-not-exist.json"}}
	if err := schemavalidation.ValidateYAML(data, configSchemaURL, bogus); err != nil {
		t.Fatalf("a resources list with no relevant entries should behave identically, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The underlying jsonschema.ValidationError survives the fmt.Errorf("%w", ...)
// wrapping, so structured (non-string) inspection is available via
// errors.As even though every current call site only does substring
// matching on Error(). Documented here as a positive finding: callers that
// need more than a prefix check are not blocked from getting it.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ValidateDocument's normalizer only deep-normalizes generic YAML-shaped
// values (map[string]any / map[any]any / []any). A concretely-typed Go doc
// passed at the *top level* gets a yaml.Marshal/Unmarshal round trip
// (normalizeDocument's default branch) that turns it into those generic
// shapes. But a concretely-typed value *nested inside* an already-generic
// map/slice does NOT get that same round trip -- normalizeYAMLValue's
// default case returns it unchanged -- so it reaches the jsonschema
// validator as e.g. a raw []string instead of []any, which the validator
// does not recognize as a JSON array at all.
//
// This only matters for direct ValidateDocument callers building documents
// out of concretely-typed Go values by hand. Every real call site in this
// repo goes through ValidateYAML, whose yaml.Unmarshal into `any` always
// produces map[string]any/[]any/scalars, so this gap is not reachable in
// production today.
// ---------------------------------------------------------------------------

func TestValidateDocument_TopLevelTypedStructIsNormalized(t *testing.T) {
	type doc struct {
		Name  string   `yaml:"name"`
		Tasks []string `yaml:"tasks"`
	}
	// A nil/empty Tasks slice round-trips through YAML as `tasks: []`, which
	// normalizes cleanly to []any{}.
	if err := schemavalidation.ValidateDocument(doc{Name: "p"}, playbookSchemaURL, playbookResources); err != nil {
		t.Fatalf("expected top-level typed struct to normalize and validate, got: %v", err)
	}
}

func TestValidateDocument_NestedTypedSliceNotNormalized(t *testing.T) {
	// KNOWN LIMITATION: a nested []string (as opposed to []any) is passed
	// through to the jsonschema validator unchanged, where it is not
	// recognized as an array at all -- producing a confusing internal-shaped
	// error rather than either validating or a normal schema violation.
	doc := map[string]any{
		"name":   "p",
		"import": []string{"a.yml"},
		"tasks":  []any{},
	}
	err := schemavalidation.ValidateDocument(doc, playbookSchemaURL, playbookResources)
	if err == nil {
		t.Fatal("expected an error: a nested []string value is not normalized to []any before validation")
	}
	if !strings.Contains(err.Error(), "invalid jsonType") {
		t.Fatalf("error = %q, want it to surface the underlying jsonschema type-shape failure (this test documents current, confusing behavior)", err)
	}
}

// TestValidateDocument_UnmarshalableGoValuePanics documents that
// normalizeDocument's yaml.Marshal fallback (for top-level values that
// aren't already map[string]any/map[any]any/[]any/a scalar) does not
// convert a marshal failure into a graceful error for every input: yaml.v3's
// Marshal PANICS (rather than returning an error) for certain Go types such
// as channels and funcs. ValidateDocument has no recover, so this panic
// propagates all the way out of the package.
//
// The panic is recovered locally here so this characterization test cannot
// take down the rest of the suite; it still proves the current contract
// violation (a library function panicking instead of returning `error` for
// caller-supplied input it can't handle).
func TestValidateDocument_UnmarshalableGoValuePanics(t *testing.T) {
	panicked := func() (recovered any) {
		defer func() { recovered = recover() }()
		_ = schemavalidation.ValidateDocument(make(chan int), playbookSchemaURL, playbookResources)
		return nil
	}()
	if panicked == nil {
		t.Fatal("expected ValidateDocument to panic on an unmarshalable Go value; if this now returns a graceful error instead, update this test to assert that instead of a panic")
	}
}

func TestValidateYAML_UnderlyingValidationErrorAccessibleViaErrorsAs(t *testing.T) {
	err := schemavalidation.ValidateYAML([]byte("tasks: 5\n"), playbookSchemaURL, playbookResources)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected errors.As to unwrap a *jsonschema.ValidationError from %v", err)
	}
	if verr.SchemaURL == "" {
		t.Error("expected the unwrapped ValidationError to carry a SchemaURL")
	}
}
