package rules

import (
	"fmt"
	"math"
	"sort"
)

// RuleMetadata describes supported config options and validations for a rule.
type RuleMetadata struct {
	ID                string
	AllowedOptionKeys []string
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
			_, ok = allowedSeverities[severity]
			return ok
		},
	},
	"suppression_selectors": {
		validate: func(value any) bool {
			switch selectors := value.(type) {
			case []interface{}:
				for _, selector := range selectors {
					if _, ok := selector.(string); !ok {
						return false
					}
				}
				return true
			case []string:
				return true
			default:
				return false
			}
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
}

// ConfigRegistry returns rule metadata keyed by rule ID.
func ConfigRegistry() map[string]RuleMetadata {
	registry := map[string]RuleMetadata{}
	for _, ruleID := range []string{
		"max-fanout",
		"no-duplicate-node-ids",
		"no-disconnected-nodes",
	} {
		allowed := make([]string, 0, len(sharedOptionConstraints)+len(ruleSpecificConstraints[ruleID]))
		for key := range sharedOptionConstraints {
			allowed = append(allowed, key)
		}
		for key := range ruleSpecificConstraints[ruleID] {
			allowed = append(allowed, key)
		}
		sort.Strings(allowed)
		registry[ruleID] = RuleMetadata{ID: ruleID, AllowedOptionKeys: allowed}
	}
	return registry
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
