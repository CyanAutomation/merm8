package rules

import "sort"

const CurrentConfigSchemaVersion = "v1"

// ConfigJSONSchema returns a JSON Schema that validates lint configuration.
//
// Canonical format:
//   - Versioned format: {"schema-version":"v1","rules":{...}}
func ConfigJSONSchema() map[string]any {
	return ConfigV1JSONSchema()
}

// ConfigV1JSONSchema returns the schema for the versioned config contract.
func ConfigV1JSONSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "merm8 Rule Configuration v1",
		"type":    "object",
		"required": []string{
			"schema-version",
			"rules",
		},
		"additionalProperties": false,
		"properties": map[string]any{
			"schema-version": map[string]any{
				"type": "string",
				"enum": []string{CurrentConfigSchemaVersion},
			},
			"rules": flatConfigSchema(),
		},
	}
}

func flatConfigSchema() map[string]any {
	registry := ConfigRegistry()
	ruleIDs := make([]string, 0, len(registry))
	for ruleID := range registry {
		ruleIDs = append(ruleIDs, ruleID)
	}
	sort.Strings(ruleIDs)

	properties := make(map[string]any, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		properties[ruleID] = ruleOptionsSchema(registry[ruleID])
	}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
}

func ruleOptionsSchema(metadata RuleMetadata) map[string]any {
	properties := make(map[string]any, len(metadata.ConfigurableOptions))
	for _, option := range metadata.ConfigurableOptions {
		properties[option.Name] = optionSchema(option.Name, option.Description)
	}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
}

func optionSchema(name, description string) map[string]any {
	schema := map[string]any{"description": description}

	switch name {
	case "enabled":
		schema["type"] = "boolean"
	case "severity":
		schema["type"] = "string"
		schema["enum"] = []string{"error", "warning", "info"}
	case "suppression-selectors":
		schema["type"] = "array"
		schema["items"] = map[string]any{"type": "string"}
	case "limit":
		schema["type"] = "integer"
		schema["minimum"] = 1
	default:
		schema["type"] = "string"
	}

	return schema
}
