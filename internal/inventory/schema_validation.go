package inventory

import "github.com/bluecadet/preflight/internal/schemavalidation"

const inventorySchemaURL = "https://preflight.dev/schema/inventory.schema.json"

var schemaResources = []schemavalidation.Resource{
	{URL: inventorySchemaURL, Path: "inventory.schema.json"},
}

// ValidateYAML validates an inventory document against the embedded JSON schema.
func ValidateYAML(data []byte) error {
	return schemavalidation.ValidateYAML(data, inventorySchemaURL, schemaResources)
}

// ValidateDocument validates an inventory value against the embedded JSON schema.
func ValidateDocument(doc any) error {
	return schemavalidation.ValidateDocument(doc, inventorySchemaURL, schemaResources)
}
