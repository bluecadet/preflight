package main

import (
	"os"
	"testing"

	"github.com/bluecadet/preflight/internal/output"
)

func TestParseFormatSupportsJSONL(t *testing.T) {
	got, err := parseFormat("jsonl", os.Stdout)
	if err != nil {
		t.Fatalf("parseFormat returned error: %v", err)
	}
	if got != output.FormatJSONL {
		t.Fatalf("parseFormat(jsonl) = %q, want %q", got, output.FormatJSONL)
	}
}

func TestParseFormatRejectsUnknownValues(t *testing.T) {
	if _, err := parseFormat("bogus", os.Stdout); err == nil {
		t.Fatal("expected parseFormat to reject unknown format")
	}
}
