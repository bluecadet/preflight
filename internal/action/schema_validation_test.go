package action

import (
	"strings"
	"testing"

	schemafiles "github.com/bluecadet/preflight/schema"
)

func TestValidatePlaybookYAML_SchemaFailure(t *testing.T) {
	err := ValidatePlaybookYAML([]byte(`
name: bad-playbook
tasks:
  - shell:
      cmd: echo
`))
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if !strings.Contains(err.Error(), "schema validation failed") {
		t.Fatalf("expected schema validation failure, got %v", err)
	}
}

func TestEmbeddedSchemasAvailable(t *testing.T) {
	t.Parallel()

	cases := []string{
		"action.schema.json",
		"playbook.schema.json",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			embedded, err := schemafiles.FS.ReadFile(name)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", name, err)
			}
			if len(embedded) == 0 {
				t.Fatalf("schema %q was empty", name)
			}
		})
	}
}
