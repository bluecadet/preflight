package action

import "github.com/bluecadet/preflight/internal/schemavalidation"

const (
	actionSchemaURL   = "https://preflight.dev/schema/action.schema.json"
	playbookSchemaURL = "https://preflight.dev/schema/playbook.schema.json"
)

// ValidateActionYAML validates an action document against the embedded JSON schema.
func ValidateActionYAML(data []byte) error {
	return schemavalidation.ValidateYAML(data, actionSchemaURL)
}

// ValidatePlaybookYAML validates a playbook document against the embedded JSON schema.
func ValidatePlaybookYAML(data []byte) error {
	return schemavalidation.ValidateYAML(data, playbookSchemaURL)
}
