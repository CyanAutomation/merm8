package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigJSONSchema_SupportsFlatAndNestedFormats(t *testing.T) {
	schema := ConfigJSONSchema()

	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected draft schema id, got %#v", schema["$schema"])
	}

	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 3 {
		t.Fatalf("expected oneOf with versioned+legacy variants, got %#v", schema["oneOf"])
	}

	versioned := variants[0].(map[string]any)
	if got := versioned["required"].([]string); len(got) != 2 || got[0] != "schema-version" || got[1] != "rules" {
		t.Fatalf("unexpected required keys on versioned schema: %#v", got)
	}
	if enumVals := versioned["properties"].(map[string]any)["schema-version"].(map[string]any)["enum"].([]string); len(enumVals) != 1 || enumVals[0] != CurrentConfigSchemaVersion {
		t.Fatalf("unexpected schema_version enum: %#v", enumVals)
	}

	flat := variants[1].(map[string]any)
	flatProps := flat["properties"].(map[string]any)
	if _, ok := flatProps["max-fanout"]; !ok {
		t.Fatal("expected max-fanout in flat properties")
	}
	if _, ok := flatProps["no-disconnected-nodes"]; !ok {
		t.Fatal("expected no-disconnected-nodes in flat properties")
	}

	nested := variants[2].(map[string]any)
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
	flat := variants[1].(map[string]any)
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

func TestConfigV1JSONSchema_MatchesVersionedArtifact(t *testing.T) {
	artifactPath := filepath.Join("..", "..", "schemas", "config.v1.json")
	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("failed to read schema artifact: %v", err)
	}

	var artifact any
	if err := json.Unmarshal(artifactBytes, &artifact); err != nil {
		t.Fatalf("failed to decode schema artifact: %v", err)
	}

	generatedBytes, err := json.Marshal(ConfigV1JSONSchema())
	if err != nil {
		t.Fatalf("failed to marshal generated schema: %v", err)
	}
	var generated any
	if err := json.Unmarshal(generatedBytes, &generated); err != nil {
		t.Fatalf("failed to decode generated schema: %v", err)
	}

	normalizedArtifact, _ := json.Marshal(artifact)
	normalizedGenerated, _ := json.Marshal(generated)
	if string(normalizedArtifact) != string(normalizedGenerated) {
		t.Fatal("schemas/config.v1.json is out of sync with ConfigV1JSONSchema")
	}
}
