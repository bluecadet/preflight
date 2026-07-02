package config

import "github.com/bluecadet/preflight/internal/schemavalidation"

const configSchemaURL = "https://preflight.dev/schema/config.schema.json"

var schemaResources = []schemavalidation.Resource{
	{URL: configSchemaURL, Path: "config.schema.json"},
}

// ValidateYAML validates a project config document against the embedded JSON schema.
func ValidateYAML(data []byte) error {
	return schemavalidation.ValidateYAML(data, configSchemaURL, schemaResources)
}
