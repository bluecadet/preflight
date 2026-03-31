package stdlib_test

import (
	"testing"

	"github.com/claytercek/preflight/internal/stdlib"
)

func TestEmbeddedActions(t *testing.T) {
	entries, err := stdlib.FS.ReadDir("actions/preflight")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected embedded stdlib actions")
	}
}

func TestAllStdlibActions(t *testing.T) {
	for _, name := range []string{"autologin"} {
		path := "actions/preflight/" + name + "/action.yml"
		data, err := stdlib.FS.ReadFile(path)
		if err != nil {
			t.Errorf("missing stdlib action %s: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("empty action file: %s", path)
		}
	}
}
