package cmd

import (
	"os"
	"strings"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/spf13/cobra"
)

// parseVars converts a slice of "key=value" strings into a map.
// Values without "=" are stored as empty strings.
func parseVars(varFlags []string) map[string]interface{} {
	result := make(map[string]interface{}, len(varFlags))
	for _, kv := range varFlags {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			result[kv] = ""
			continue
		}
		result[kv[:idx]] = kv[idx+1:]
	}
	return result
}

// getOutputFormat reads the --output flag and returns the corresponding Format.
// When the flag is not set (or is the default "text"), AutoDetect is called to
// automatically use FormatTUI when running interactively.
func getOutputFormat(cmd *cobra.Command) output.Format {
	f, _ := cmd.Flags().GetString("output")
	switch output.Format(f) {
	case output.FormatJSON, output.FormatJSONL:
		return output.Format(f)
	case output.FormatTUI:
		return output.FormatTUI
	default:
		return output.AutoDetect(os.Stdout)
	}
}

// getPlaybookPath returns the first element of args (the playbook path).
func getPlaybookPath(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
