package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigJSONSchema_UsesCanonicalVersionedFormat(t *testing.T) {
	schema := ConfigJSONSchema()

	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected draft schema id, got %#v", schema["$schema"])
	}

	if _, ok := schema["oneOf"]; ok {
		t.Fatalf("did not expect legacy oneOf variants in strict schema: %#v", schema["oneOf"])
	}

	required := schema["required"].([]string)
	if len(required) != 2 || required[0] != "schema-version" || required[1] != "rules" {
		t.Fatalf("unexpected required keys on schema: %#v", required)
	}
	if enumVals := schema["properties"].(map[string]any)["schema-version"].(map[string]any)["enum"].([]string); len(enumVals) != 1 || enumVals[0] != CurrentConfigSchemaVersion {
		t.Fatalf("unexpected schema-version enum: %#v", enumVals)
	}

	rulesProperty := schema["properties"].(map[string]any)["rules"].(map[string]any)
	rulesProps := rulesProperty["properties"].(map[string]any)
	if _, ok := rulesProps["max-fanout"]; !ok {
		t.Fatal("expected max-fanout in rules properties")
	}
	if _, ok := rulesProps["no-disconnected-nodes"]; !ok {
		t.Fatal("expected no-disconnected-nodes in rules properties")
	}
	if _, ok := rulesProps["no-duplicate-node-ids"]; !ok {
		t.Fatal("expected no-duplicate-node-ids in rules properties")
	}
	if _, ok := rulesProps["max-depth"]; !ok {
		t.Fatal("expected max-depth in rules properties")
	}
	if _, ok := rulesProps["no-cycles"]; !ok {
		t.Fatal("expected no-cycles in rules properties")
	}
}

func TestConfigJSONSchema_EncodesAllowedOptionsAndConstraints(t *testing.T) {
	schema := ConfigJSONSchema()
	rulesProps := schema["properties"].(map[string]any)["rules"].(map[string]any)["properties"].(map[string]any)

	maxFanout := rulesProps["max-fanout"].(map[string]any)
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

	nonMaxFanout := rulesProps["no-disconnected-nodes"].(map[string]any)
	nonMaxFanoutProps := nonMaxFanout["properties"].(map[string]any)
	if _, ok := nonMaxFanoutProps["limit"]; ok {
		t.Fatal("did not expect limit option on no-disconnected-nodes")
	}
}

func TestConfigJSONSchema_SeverityEnumDoesNotIncludeWarnAlias(t *testing.T) {
	schema := ConfigJSONSchema()
	rulesProps := schema["properties"].(map[string]any)["rules"].(map[string]any)["properties"].(map[string]any)
	maxFanoutProps := rulesProps["max-fanout"].(map[string]any)["properties"].(map[string]any)
	severity := maxFanoutProps["severity"].(map[string]any)
	enumVals := severity["enum"].([]string)
	for _, v := range enumVals {
		if v == "warn" {
			t.Fatalf("warn alias should not appear in schema enum: %#v", enumVals)
		}
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
