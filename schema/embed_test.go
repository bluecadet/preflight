package schema_test

import (
	"testing"

	schemafiles "github.com/bluecadet/preflight/schema"
)

func TestEmbeddedSchemasAvailable(t *testing.T) {
	t.Parallel()

	cases := []string{
		"action.schema.json",
		"playbook.schema.json",
		"inventory.schema.json",
		"config.schema.json",
		"runlog.schema.json",
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
