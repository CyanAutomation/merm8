package rules

import (
	"fmt"
	"math"
	"sort"
)

// OptionMetadata documents a configurable rule option.
type OptionMetadata struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Constraints string `json:"constraints,omitempty"`
}

// RuleMetadata describes supported config options, defaults, and validations for a rule.
type RuleMetadata struct {
	ID                  string                 `json:"id"`
	State               string                 `json:"state"`
	Availability        string                 `json:"availability,omitempty"`
	Severity            string                 `json:"severity"`
	Description         string                 `json:"description"`
	DefaultConfig       map[string]interface{} `json:"default-config"`
	ConfigurableOptions []OptionMetadata       `json:"configurable-options"`
	AllowedOptionKeys   []string               `json:"-"`
}

type optionConstraint struct {
	validate func(any) bool
}

var sharedOptionConstraints = map[string]optionConstraint{
	"enabled": {
		validate: func(value any) bool {
			_, ok := value.(bool)
			return ok
		},
	},
	"severity": {
		validate: func(value any) bool {
			severity, ok := value.(string)
			if !ok {
				return false
			}
			_, ok = allowedSeverities[normalizeSeverity(severity)]
			return ok
		},
	},
	"suppression-selectors": {
		validate: func(value any) bool {
			selectors := make([]string, 0)
			switch typedSelectors := value.(type) {
			case []interface{}:
				selectors = make([]string, 0, len(typedSelectors))
				for _, selector := range typedSelectors {
					selectorValue, ok := selector.(string)
					if !ok {
						return false
					}
					selectors = append(selectors, selectorValue)
				}
			case []string:
				selectors = typedSelectors
			default:
				return false
			}

			for _, selector := range selectors {
				if _, ok := ParseSuppressionSelector(selector); !ok {
					return false
				}
			}

			return true
		},
	},
}

var ruleSpecificConstraints = map[string]map[string]optionConstraint{
	"max-fanout": {
		"limit": {
			validate: func(value any) bool {
				switch n := value.(type) {
				case int:
					return n >= 1
				case float64:
					return n >= 1 && n == math.Trunc(n)
				default:
					return false
				}
			},
		},
	},

	"max-depth": {
		"limit": {
			validate: func(value any) bool {
				switch n := value.(type) {
				case int:
					return n >= 1
				case float64:
					return n >= 1 && n == math.Trunc(n)
				default:
					return false
				}
			},
		},
	},
	"no-cycles": {
		"allow-self-loop": {
			validate: func(value any) bool {
				_, ok := value.(bool)
				return ok
			},
		},
	},
}

var builtInRuleMetadata = []RuleMetadata{
	{
		ID:          "max-fanout",
		State:       "implemented",
		Severity:    "warning",
		Description: "Flags nodes whose outgoing edge count exceeds a configurable limit.",
		DefaultConfig: map[string]interface{}{
			"enabled":               true,
			"severity":              "warning",
			"suppression-selectors": []string{},
			"limit":                 defaultMaxFanout,
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
			{Name: "limit", Type: "integer", Description: "Maximum allowed outgoing edges per node.", Constraints: "Must be an integer >= 1. Default is 5."},
		},
	},
	{
		ID:          "no-duplicate-node-ids",
		State:       "implemented",
		Severity:    "error",
		Description: "Flags diagrams containing more than one node with the same ID.",
		DefaultConfig: map[string]interface{}{
			"enabled":               true,
			"severity":              "error",
			"suppression-selectors": []string{},
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
		},
	},
	{
		ID:          "no-disconnected-nodes",
		State:       "implemented",
		Severity:    "error",
		Description: "Flags nodes that are not connected by any incoming or outgoing edge.",
		DefaultConfig: map[string]interface{}{
			"enabled":               true,
			"severity":              "error",
			"suppression-selectors": []string{},
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
		},
	},
	{
		ID:          "max-depth",
		State:       "implemented",
		Severity:    "warning",
		Description: "Flags root-to-leaf traversals whose depth exceeds a configurable limit.",
		DefaultConfig: map[string]interface{}{
			"enabled":               true,
			"severity":              "warning",
			"suppression-selectors": []string{},
			"limit":                 defaultMaxDepth,
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
			{Name: "limit", Type: "integer", Description: "Maximum allowed depth for root-to-leaf traversals.", Constraints: "Must be an integer >= 1. Default is 8."},
		},
	},
	{
		ID:          "no-cycles",
		State:       "implemented",
		Severity:    "error",
		Description: "Flags directed cycles in flowcharts.",
		DefaultConfig: map[string]interface{}{
			"enabled":               true,
			"severity":              "error",
			"suppression-selectors": []string{},
			"allow-self-loop":       false,
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
			{Name: "allow-self-loop", Type: "boolean", Description: "Allow single-node self-loop cycles without emitting issues.", Constraints: "Must be true or false."},
		},
	},
	{
		ID:           "class-no-orphan-classes",
		State:        "planned",
		Availability: "Planned for class diagram linting expansion.",
		Severity:     "info",
		Description:  "Will flag classes that are not connected to any relationship.",
		DefaultConfig: map[string]interface{}{
			"enabled":               false,
			"severity":              "info",
			"suppression-selectors": []string{},
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
		},
	},
	{
		ID:           "er-no-isolated-entities",
		State:        "planned",
		Availability: "Planned for ER diagram linting expansion.",
		Severity:     "info",
		Description:  "Will flag entities with no incoming or outgoing relationships.",
		DefaultConfig: map[string]interface{}{
			"enabled":               false,
			"severity":              "info",
			"suppression-selectors": []string{},
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
		},
	},
	{
		ID:           "sequence-max-participants",
		State:        "planned",
		Availability: "Planned for sequence diagram linting expansion.",
		Severity:     "info",
		Description:  "Will flag sequence diagrams that exceed a configurable participant count.",
		DefaultConfig: map[string]interface{}{
			"enabled":               false,
			"severity":              "info",
			"suppression-selectors": []string{},
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
		},
	},
	{
		ID:           "state-no-unreachable-states",
		State:        "planned",
		Availability: "Planned for state diagram linting expansion.",
		Severity:     "info",
		Description:  "Will flag states that cannot be reached from any initial state.",
		DefaultConfig: map[string]interface{}{
			"enabled":               false,
			"severity":              "info",
			"suppression-selectors": []string{},
		},
		ConfigurableOptions: []OptionMetadata{
			{Name: "enabled", Type: "boolean", Description: "Enable or disable this rule.", Constraints: "Must be true or false."},
			{Name: "severity", Type: "string", Description: "Severity assigned to emitted issues (case-insensitive; surrounding whitespace ignored).", Constraints: "Accepted values (canonical): error, warning, info."},
			{Name: "suppression-selectors", Type: "array[string]", Description: "Selectors that suppress matching issues.", Constraints: "Each entry must be a string selector."},
		},
	},
}

// ListRuleMetadata returns all built-in rule metadata sorted by rule ID.
func ListRuleMetadata() []RuleMetadata {
	metadata := make([]RuleMetadata, 0, len(builtInRuleMetadata))
	for _, rule := range builtInRuleMetadata {
		metadata = append(metadata, cloneRuleMetadata(rule))
	}
	sort.Slice(metadata, func(i, j int) bool {
		return metadata[i].ID < metadata[j].ID
	})
	return metadata
}

// ListRuleMetadataForRuleIDs returns built-in metadata for the provided
// implemented rule IDs, sorted by rule ID.
func ListRuleMetadataForRuleIDs(ruleIDs map[string]struct{}) []RuleMetadata {
	if len(ruleIDs) == 0 {
		return []RuleMetadata{}
	}

	metadata := make([]RuleMetadata, 0, len(ruleIDs))
	for _, rule := range builtInRuleMetadata {
		if _, ok := ruleIDs[rule.ID]; !ok {
			continue
		}
		metadata = append(metadata, cloneRuleMetadata(rule))
	}

	sort.Slice(metadata, func(i, j int) bool {
		return metadata[i].ID < metadata[j].ID
	})
	return metadata
}

// ListImplementedRuleMetadata returns metadata for implemented built-in rules.
func ListImplementedRuleMetadata() []RuleMetadata {
	metadata := make([]RuleMetadata, 0, len(builtInRuleMetadata))
	for _, rule := range builtInRuleMetadata {
		if rule.State != "implemented" {
			continue
		}
		metadata = append(metadata, cloneRuleMetadata(rule))
	}
	sort.Slice(metadata, func(i, j int) bool {
		return metadata[i].ID < metadata[j].ID
	})
	return metadata
}

// ConfigRegistry returns rule metadata keyed by rule ID.
func ConfigRegistry() map[string]RuleMetadata {
	return ConfigRegistryForRuleIDs(nil)
}

// ConfigRegistryForRuleIDs returns rule metadata keyed by rule ID for the
// provided implemented rule IDs. A nil/empty ruleIDs set returns all metadata.
func ConfigRegistryForRuleIDs(ruleIDs map[string]struct{}) map[string]RuleMetadata {
	filtered := ListImplementedRuleMetadata()
	if len(ruleIDs) > 0 {
		filtered = ListRuleMetadataForRuleIDs(ruleIDs)
	}

	registry := map[string]RuleMetadata{}
	for _, metadata := range filtered {
		allowed := make([]string, 0, len(sharedOptionConstraints)+len(ruleSpecificConstraints[metadata.ID]))
		for key := range sharedOptionConstraints {
			allowed = append(allowed, key)
		}
		for key := range ruleSpecificConstraints[metadata.ID] {
			allowed = append(allowed, key)
		}
		sort.Strings(allowed)
		metadata.AllowedOptionKeys = allowed
		registry[metadata.ID] = metadata
	}
	return registry
}

func cloneRuleMetadata(metadata RuleMetadata) RuleMetadata {
	copyMetadata := metadata
	copyMetadata.DefaultConfig = make(map[string]interface{}, len(metadata.DefaultConfig))
	for key, value := range metadata.DefaultConfig {
		copyMetadata.DefaultConfig[key] = value
	}
	copyMetadata.ConfigurableOptions = append([]OptionMetadata(nil), metadata.ConfigurableOptions...)
	copyMetadata.AllowedOptionKeys = append([]string(nil), metadata.AllowedOptionKeys...)
	return copyMetadata
}

// NormalizeOptionKey normalizes option key aliases to their canonical key.
func NormalizeOptionKey(key string) string {
	return normalizeMetaKey(key)
}

// ValidateOption validates a single rule option value.
func ValidateOption(ruleID, optionKey string, value any) error {
	canonicalKey := NormalizeOptionKey(optionKey)
	constraint, ok := sharedOptionConstraints[canonicalKey]
	if !ok {
		ruleConstraints := ruleSpecificConstraints[ruleID]
		constraint, ok = ruleConstraints[canonicalKey]
		if !ok {
			return fmt.Errorf("unknown option")
		}
	}

	if !constraint.validate(value) {
		return fmt.Errorf("invalid option")
	}
	return nil
}
