package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
)

type quietCommandError struct {
	err error
}

func (e quietCommandError) Error() string { return e.err.Error() }
func (e quietCommandError) Unwrap() error { return e.err }
func (e quietCommandError) Quiet() bool   { return true }

func quietError(err error) error {
	if err == nil {
		return nil
	}
	return quietCommandError{err: err}
}

func showScreen(_ *cobra.Command, screen output.Screen) error {
	return output.RunScreenTUI(os.Stdout, output.Options{Input: os.Stdin}, screen)
}

func prettyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func compactSummary(value any) string {
	if value == nil {
		return "(none)"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func formatTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return "(never)"
	}
	return ts.UTC().Format("2006-01-02 15:04:05 UTC")
}

func comparisonTone(status runner.ComparisonStatus) string {
	switch status {
	case runner.ComparisonStatusUnchanged:
		return "ok"
	case runner.ComparisonStatusChanged, runner.ComparisonStatusNew:
		return "changed"
	case runner.ComparisonStatusRemoved:
		return "failed"
	case runner.ComparisonStatusStatusOnly:
		return "warning"
	default:
		return "info"
	}
}
