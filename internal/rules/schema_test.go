package rules

import "testing"

func TestConfigJSONSchema_SupportsFlatAndNestedFormats(t *testing.T) {
	schema := ConfigJSONSchema()

	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected draft schema id, got %#v", schema["$schema"])
	}

	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 2 {
		t.Fatalf("expected oneOf with flat+nested variants, got %#v", schema["oneOf"])
	}

	flat := variants[0].(map[string]any)
	flatProps := flat["properties"].(map[string]any)
	if _, ok := flatProps["max-fanout"]; !ok {
		t.Fatal("expected max-fanout in flat properties")
	}
	if _, ok := flatProps["no-disconnected-nodes"]; !ok {
		t.Fatal("expected no-disconnected-nodes in flat properties")
	}

	nested := variants[1].(map[string]any)
	nestedProps := nested["properties"].(map[string]any)
	rulesProperty := nestedProps["rules"].(map[string]any)
	rulesProps := rulesProperty["properties"].(map[string]any)
	if _, ok := rulesProps["no-duplicate-node-ids"]; !ok {
		t.Fatal("expected no-duplicate-node-ids in nested rules properties")
	}
}

func TestConfigJSONSchema_EncodesAllowedOptionsAndConstraints(t *testing.T) {
	schema := ConfigJSONSchema()
	variants := schema["oneOf"].([]any)
	flat := variants[0].(map[string]any)
	flatProps := flat["properties"].(map[string]any)

	maxFanout := flatProps["max-fanout"].(map[string]any)
	maxFanoutProps := maxFanout["properties"].(map[string]any)

	if got := maxFanout["additionalProperties"]; got != false {
		t.Fatalf("expected max-fanout additionalProperties=false, got %#v", got)
	}

	severity := maxFanoutProps["severity"].(map[string]any)
	enumVals := severity["enum"].([]string)
	if len(enumVals) != 3 || enumVals[0] != "error" || enumVals[1] != "warning" || enumVals[2] != "info" {
		t.Fatalf("unexpected severity enum: %#v", enumVals)
	}

	limit := maxFanoutProps["limit"].(map[string]any)
	if limit["type"] != "integer" || limit["minimum"] != 1 {
		t.Fatalf("unexpected max-fanout limit schema: %#v", limit)
	}

	nonMaxFanout := flatProps["no-disconnected-nodes"].(map[string]any)
	nonMaxFanoutProps := nonMaxFanout["properties"].(map[string]any)
	if _, ok := nonMaxFanoutProps["limit"]; ok {
		t.Fatal("did not expect limit option on no-disconnected-nodes")
	}
}
