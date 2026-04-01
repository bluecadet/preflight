package runner

import "testing"

func TestSummarizeParamsDoesNotRedactOrdinaryPathValues(t *testing.T) {
	params := map[string]any{
		"path": `C:\Exhibits\Lobby`,
		"url":  "https://example.com/app",
	}

	summary, ok := SummarizeParams(params).(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", SummarizeParams(params))
	}
	if summary["path"] != `C:\Exhibits\Lobby` {
		t.Fatalf("expected path to remain visible, got %#v", summary["path"])
	}
	if summary["url"] != "https://example.com/app" {
		t.Fatalf("expected url to remain visible, got %#v", summary["url"])
	}
}
