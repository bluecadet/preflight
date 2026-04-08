package main

import (
	"os"
	"testing"
)

func TestParseFormatRejectsUnknownValues(t *testing.T) {
	if _, err := parseFormat("bogus", os.Stdout); err == nil {
		t.Fatal("expected parseFormat to reject unknown format")
	}
}
