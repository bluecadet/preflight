package target

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	schemafiles "github.com/bluecadet/preflight/schema"
)

// TestCatalogMatchesModuleSchema asserts every catalog module is an inline
// task field in the shared module schema, and vice versa.
func TestCatalogMatchesModuleSchema(t *testing.T) {
	data, err := schemafiles.FS.ReadFile("modules.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs struct {
			Task struct {
				Properties map[string]struct {
					Ref string `json:"$ref"`
				} `json:"properties"`
			} `json:"task"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	var inSchema []string
	for name, prop := range doc.Defs.Task.Properties {
		if strings.HasPrefix(prop.Ref, "#/$defs/module") {
			inSchema = append(inSchema, name)
		}
	}
	slices.Sort(inSchema)
	want := CatalogNames()
	slices.Sort(want)
	if !slices.Equal(inSchema, want) {
		t.Errorf("schema task modules %v != catalog %v", inSchema, want)
	}
}
