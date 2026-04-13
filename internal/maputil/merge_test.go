package maputil_test

import (
	"testing"

	"github.com/bluecadet/preflight/internal/maputil"
)

func TestDeepMerge_Disjoint(t *testing.T) {
	dst := map[string]any{"left": 1}
	src := map[string]any{"right": 2}

	maputil.DeepMerge(dst, src)

	if dst["left"] != 1 || dst["right"] != 2 {
		t.Fatalf("merged map = %#v, want both keys preserved", dst)
	}
}

func TestDeepMerge_Overwrite(t *testing.T) {
	dst := map[string]any{"key": "old"}
	src := map[string]any{"key": "new"}

	maputil.DeepMerge(dst, src)

	if got := dst["key"]; got != "new" {
		t.Fatalf("dst[key] = %#v, want %q", got, "new")
	}
}

func TestDeepMerge_NestedMerge(t *testing.T) {
	dst := map[string]any{"outer": map[string]any{"a": 1}}
	src := map[string]any{"outer": map[string]any{"b": 2}}

	maputil.DeepMerge(dst, src)

	outer, ok := dst["outer"].(map[string]any)
	if !ok {
		t.Fatalf("dst[outer] = %#v, want map[string]any", dst["outer"])
	}
	if outer["a"] != 1 || outer["b"] != 2 {
		t.Fatalf("outer = %#v, want merged nested map", outer)
	}
}

func TestDeepMerge_NestedOverwrite(t *testing.T) {
	dst := map[string]any{"key": map[string]any{"a": 1, "b": 2}}
	src := map[string]any{"key": map[string]any{"b": 3, "c": 4}}

	maputil.DeepMerge(dst, src)

	got := dst["key"].(map[string]any)
	want := map[string]any{"a": 1, "b": 3, "c": 4}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d; got = %#v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got[%q] = %#v, want %#v; full map = %#v", k, got[k], v, got)
		}
	}
}

func TestDeepMerge_SrcMapOverwritesDstScalar(t *testing.T) {
	dst := map[string]any{"key": "string"}
	src := map[string]any{"key": map[string]any{"nested": "map"}}

	maputil.DeepMerge(dst, src)

	got, ok := dst["key"].(map[string]any)
	if !ok {
		t.Fatalf("dst[key] = %#v, want map[string]any", dst["key"])
	}
	if got["nested"] != "map" {
		t.Fatalf("nested value = %#v, want %q", got["nested"], "map")
	}
}

func TestDeepMerge_DstMapOverwrittenBySrcScalar(t *testing.T) {
	dst := map[string]any{"key": map[string]any{"nested": "map"}}
	src := map[string]any{"key": "string"}

	maputil.DeepMerge(dst, src)

	if got := dst["key"]; got != "string" {
		t.Fatalf("dst[key] = %#v, want %q", got, "string")
	}
}

func TestDeepMerge_EmptySrc(t *testing.T) {
	dst := map[string]any{"key": "value"}

	maputil.DeepMerge(dst, map[string]any{})

	if len(dst) != 1 || dst["key"] != "value" {
		t.Fatalf("dst = %#v, want unchanged map", dst)
	}
}

func TestDeepMerge_EmptyDst(t *testing.T) {
	dst := map[string]any{}
	src := map[string]any{"key": "value", "count": 2}

	maputil.DeepMerge(dst, src)

	if len(dst) != len(src) || dst["key"] != "value" || dst["count"] != 2 {
		t.Fatalf("dst = %#v, want %#v", dst, src)
	}
}

func TestDeepMerge_DeepNesting(t *testing.T) {
	dst := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": map[string]any{"left": 1},
			},
		},
	}
	src := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": map[string]any{"right": 2},
			},
		},
	}

	maputil.DeepMerge(dst, src)

	level1 := dst["level1"].(map[string]any)
	level2 := level1["level2"].(map[string]any)
	level3 := level2["level3"].(map[string]any)
	if level3["left"] != 1 || level3["right"] != 2 {
		t.Fatalf("level3 = %#v, want merged deep map", level3)
	}
}

func TestDeepMerge_DoesNotMutateSrc(t *testing.T) {
	src := map[string]any{
		"parent": map[string]any{
			"child": map[string]any{"value": 1},
			"added": "from-src",
		},
	}
	dst := map[string]any{
		"parent": map[string]any{
			"child": map[string]any{"existing": true},
		},
	}

	maputil.DeepMerge(dst, src)

	srcParent := src["parent"].(map[string]any)
	srcChild := srcParent["child"].(map[string]any)
	if len(srcParent) != 2 || srcParent["added"] != "from-src" {
		t.Fatalf("source parent map was mutated: %#v", srcParent)
	}
	if len(srcChild) != 1 || srcChild["value"] != 1 {
		t.Fatalf("source child map was mutated: %#v", srcChild)
	}
}
