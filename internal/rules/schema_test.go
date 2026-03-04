package rules

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestConfigJSONSchema_UsesMigrationOneOfWithFlatAndVersionedForms(t *testing.T) {
	schema := ConfigJSONSchema()

	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected draft schema id, got %#v", schema["$schema"])
	}

	oneOf, ok := schema["oneOf"].([]any)
	if !ok {
		t.Fatalf("expected oneOf variants in migration schema, got %#v", schema["oneOf"])
	}
	if len(oneOf) != 2 {
		t.Fatalf("expected two oneOf variants (flat + versioned), got %d", len(oneOf))
	}

	versionedSchema, ok := oneOf[1].(map[string]any)
	if !ok {
		t.Fatalf("expected versioned variant to be an object schema, got %#v", oneOf[1])
	}
	required, ok := versionedSchema["required"].([]string)
	if !ok || len(required) != 2 || required[0] != "schema-version" || required[1] != "rules" {
		t.Fatalf("unexpected required keys on versioned schema: %#v", versionedSchema["required"])
	}
}

func TestConfigJSONSchema_EncodesAllowedOptionsAndConstraints(t *testing.T) {
	schema := ConfigV1JSONSchema()
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

	suppressionSelectors := maxFanoutProps["suppression-selectors"].(map[string]any)
	items := suppressionSelectors["items"].(map[string]any)
	if items["pattern"] != SuppressionSelectorPattern {
		t.Fatalf("unexpected suppression selector pattern: %#v", items["pattern"])
	}

	nonMaxFanout := rulesProps["no-disconnected-nodes"].(map[string]any)
	nonMaxFanoutProps := nonMaxFanout["properties"].(map[string]any)
	if _, ok := nonMaxFanoutProps["limit"]; ok {
		t.Fatal("did not expect limit option on no-disconnected-nodes")
	}
}

func TestConfigJSONSchema_SeverityEnumDoesNotIncludeWarnAlias(t *testing.T) {
	schema := ConfigV1JSONSchema()
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

func TestConfigJSONSchema_ValidationAcceptsFlatAndVersionedConfigs(t *testing.T) {
	schema := ConfigJSONSchema()
	validConfigs := []string{
		`{"max-fanout":{"limit":1},"no-cycles":{"enabled":true}}`,
		`{"schema-version":"v1","rules":{"max-fanout":{"limit":3},"no-cycles":{"enabled":false}}}`,
	}

	for _, cfg := range validConfigs {
		cfg := cfg
		t.Run(cfg, func(t *testing.T) {
			if err := validateConfigJSON(schema, cfg); err != nil {
				t.Fatalf("expected valid config, got error: %v", err)
			}
		})
	}
}

func TestConfigJSONSchema_ValidationRejectsInvalidConfigs(t *testing.T) {
	schema := ConfigJSONSchema()
	invalidConfigs := []string{
		`{"max-fanout":{"limit":-1}}`,
		`{"schema-version":"v1","rules":{"max-fanout":{"limit":-1}}}`,
		`{"max-fanout":{"limit":"3"}}`,
		`{"schema-version":"v1","rules":{"max-fanout":{"limit":"3"}}}`,
		`{"max-fanout":{"limit":0}}`,
		`{"max-fanout":{"suppression-selectors":["node"]}}`,
		`{"max-fanout":{"suppression-selectors":["! node:A"]}}`,
		`{"max-fanout":{"suppression-selectors":["node:"]}}`,
	}

	for _, cfg := range invalidConfigs {
		cfg := cfg
		t.Run(cfg, func(t *testing.T) {
			if err := validateConfigJSON(schema, cfg); err == nil {
				t.Fatalf("expected validation error for config: %s", cfg)
			}
		})
	}
}

func validateConfigJSON(schema map[string]any, configJSON string) error {
	var instance any
	if err := json.Unmarshal([]byte(configJSON), &instance); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return validateSchemaNode(schema, instance)
}

func validateSchemaNode(schema map[string]any, instance any) error {
	if oneOfRaw, ok := schema["oneOf"]; ok {
		variants, ok := oneOfRaw.([]any)
		if !ok {
			return fmt.Errorf("schema oneOf is not []any")
		}
		matched := 0
		for _, variant := range variants {
			variantSchema, ok := variant.(map[string]any)
			if !ok {
				continue
			}
			if err := validateSchemaNode(variantSchema, instance); err == nil {
				matched++
			}
		}
		if matched != 1 {
			return fmt.Errorf("oneOf mismatch: matched=%d", matched)
		}
		return nil
	}

	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object":
		obj, ok := instance.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		required, err := stringSlice(schema["required"])
		if err != nil {
			return err
		}
		for _, key := range required {
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("missing required key %q", key)
			}
		}
		props, _ := schema["properties"].(map[string]any)
		if schema["additionalProperties"] == false {
			for key := range obj {
				if _, ok := props[key]; !ok {
					return fmt.Errorf("unexpected property %q", key)
				}
			}
		}
		for key, val := range obj {
			if propSchemaRaw, ok := props[key]; ok {
				propSchema, ok := propSchemaRaw.(map[string]any)
				if !ok {
					continue
				}
				if err := validateSchemaNode(propSchema, val); err != nil {
					return fmt.Errorf("property %q: %w", key, err)
				}
			}
		}
	case "integer":
		num, ok := instance.(float64)
		if !ok {
			return fmt.Errorf("expected integer")
		}
		if math.Mod(num, 1) != 0 {
			return fmt.Errorf("expected integral number")
		}
		if minRaw, ok := schema["minimum"]; ok {
			min, ok := minRaw.(float64)
			if !ok {
				if i, ok := minRaw.(int); ok {
					min = float64(i)
				} else {
					return fmt.Errorf("invalid minimum type %T", minRaw)
				}
			}
			if num < min {
				return fmt.Errorf("number %v below minimum %v", num, min)
			}
		}
	case "string":
		str, ok := instance.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		if patternRaw, ok := schema["pattern"]; ok {
			pattern, ok := patternRaw.(string)
			if !ok {
				return fmt.Errorf("pattern must be string")
			}
			if matched, err := regexp.MatchString(pattern, str); err != nil {
				return fmt.Errorf("invalid pattern %q: %w", pattern, err)
			} else if !matched {
				return fmt.Errorf("string %q does not match pattern %q", str, pattern)
			}
		}
		if enumRaw, ok := schema["enum"]; ok {
			enumVals, err := stringSlice(enumRaw)
			if err != nil {
				return err
			}
			matched := false
			for _, allowed := range enumVals {
				if str == allowed {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("string %q not in enum", str)
			}
		}
	case "boolean":
		if _, ok := instance.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "array":
		arr, ok := instance.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for i, item := range arr {
			if err := validateSchemaNode(itemSchema, item); err != nil {
				return fmt.Errorf("array item %d: %w", i, err)
			}
		}
	}

	return nil
}

func stringSlice(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	if vals, ok := raw.([]string); ok {
		return vals, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected []string/[]any, got %T", raw)
	}
	vals := make([]string, 0, len(items))
	for _, item := range items {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("expected string item, got %T", item)
		}
		vals = append(vals, str)
	}
	return vals, nil
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
