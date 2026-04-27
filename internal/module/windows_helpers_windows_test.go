//go:build windows

package module

import "testing"

func TestParseWindowsBool_MultilineOutputUsesLastLine(t *testing.T) {
	got, err := parseWindowsBool([]byte("Installed package is not available from any source: Foo\r\ntrue\r\n"))
	if err != nil {
		t.Fatalf("parseWindowsBool returned error: %v", err)
	}
	if !got {
		t.Fatalf("parseWindowsBool = %v, want true", got)
	}
}

func TestNormalizeWindowsOutputLine_StripsCarriageReturnProgress(t *testing.T) {
	got := normalizeWindowsOutputLine("- \r                                                                                                                        \rInstalled package is not available from any source: Foo")
	want := "Installed package is not available from any source: Foo"
	if got != want {
		t.Fatalf("normalizeWindowsOutputLine = %q, want %q", got, want)
	}
}
