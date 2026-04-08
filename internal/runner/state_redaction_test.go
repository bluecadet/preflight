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

func TestStateParamSummaryRedactsSecretRefUnderNeutralKey(t *testing.T) {
	source := map[string]any{
		"cmd": "secret:db-password",
	}
	resolved := map[string]any{
		"cmd": "hunter2",
	}

	summary, ok := StateParamSummary(source, resolved).(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", StateParamSummary(source, resolved))
	}
	if summary["cmd"] != "[redacted]" {
		t.Fatalf("expected secret-derived neutral key to be redacted, got %#v", summary["cmd"])
	}
}

func TestStateParamHashIgnoresSecretContentChanges(t *testing.T) {
	source := map[string]any{
		"cmd": "secret:db-password",
	}
	first := map[string]any{
		"cmd": "hunter2",
	}
	second := map[string]any{
		"cmd": "correct horse battery staple",
	}

	if got, want := StateParamHash(source, first), StateParamHash(source, second); got != want {
		t.Fatalf("expected secret-derived hashes to match, got %q != %q", got, want)
	}
}

func TestStateParamSummaryRedactsPasswordSecretRef(t *testing.T) {
	source := map[string]any{
		"password": "secret:db-password",
	}
	resolved := map[string]any{
		"password": "hunter2",
	}

	summary, ok := StateParamSummary(source, resolved).(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", StateParamSummary(source, resolved))
	}
	if summary["password"] != "[redacted]" {
		t.Fatalf("expected password to be redacted, got %#v", summary["password"])
	}
}
