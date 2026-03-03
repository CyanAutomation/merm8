package rules

import "sort"

// ConfigJSONSchema returns a JSON Schema that validates lint configuration.
//
// The schema accepts both supported payload formats:
//   - Flat: {"rule-id": {...}}
//   - Nested: {"rules": {"rule-id": {...}}}
func ConfigJSONSchema() map[string]any {
	flatConfig := flatConfigSchema()
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "merm8 Rule Configuration",
		"type":    "object",
		"oneOf": []any{
			flatConfig,
			map[string]any{
				"type":                 "object",
				"required":             []string{"rules"},
				"additionalProperties": false,
				"properties": map[string]any{
					"rules": flatConfig,
				},
			},
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
		properties[ruleID] = ruleOptionsSchema(ruleID)
	}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
}

func ruleOptionsSchema(ruleID string) map[string]any {
	properties := map[string]any{
		"enabled": map[string]any{
			"type":        "boolean",
			"description": "Enable or disable this rule.",
		},
		"severity": map[string]any{
			"type":        "string",
			"enum":        []string{"error", "warning", "info"},
			"description": "Severity assigned to emitted issues.",
		},
		"suppression_selectors": map[string]any{
			"type":        "array",
			"description": "Selectors that suppress matching issues.",
			"items": map[string]any{
				"type": "string",
			},
		},
	}

	if ruleID == "max-fanout" {
		properties["limit"] = map[string]any{
			"type":        "integer",
			"minimum":     1,
			"description": "Maximum allowed outgoing edges per node.",
		}
	}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
}
