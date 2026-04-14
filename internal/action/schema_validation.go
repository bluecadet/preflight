package action

import "github.com/bluecadet/preflight/internal/schemavalidation"

const (
	actionSchemaURL   = "https://preflight.dev/schema/action.schema.json"
	playbookSchemaURL = "https://preflight.dev/schema/playbook.schema.json"
)

var schemaResources = []schemavalidation.Resource{
	{URL: actionSchemaURL, Path: "action.schema.json"},
	{URL: playbookSchemaURL, Path: "playbook.schema.json"},
}

// ValidateActionYAML validates an action document against the embedded JSON schema.
func ValidateActionYAML(data []byte) error {
	return schemavalidation.ValidateYAML(data, actionSchemaURL, schemaResources)
}

// ValidateActionDocument validates an action value against the action schema.
func ValidateActionDocument(doc any) error {
	return schemavalidation.ValidateDocument(doc, actionSchemaURL, schemaResources)
}

// ValidatePlaybookYAML validates a playbook document against the embedded JSON schema.
func ValidatePlaybookYAML(data []byte) error {
	return schemavalidation.ValidateYAML(data, playbookSchemaURL, schemaResources)
}

// ValidatePlaybookDocument validates a playbook value against the playbook schema.
func ValidatePlaybookDocument(doc any) error {
	return schemavalidation.ValidateDocument(doc, playbookSchemaURL, schemaResources)
}
