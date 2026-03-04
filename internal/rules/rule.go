// Package rules defines the Rule interface and all built-in lint rules.
package rules

import (
	"fmt"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
)

// Config holds per-rule configuration supplied by the caller.
type Config map[string]map[string]interface{}

var allowedSeverities = map[string]struct{}{
	"error":   {},
	"warning": {},
	"info":    {},
}

func normalizeSeverity(severity string) string {
	return strings.ToLower(strings.TrimSpace(severity))
}

// resolveSeverity returns the configured severity for the given rule ID, or
// defaultSeverity when no override is present.
//
// Allowed severity values are: error, warning, info.
func resolveSeverity(ruleID string, cfg Config, defaultSeverity string) (string, error) {
	ruleConfig, ok := cfg[ruleID]
	if !ok {
		return defaultSeverity, nil
	}

	rawSeverity, ok := ruleConfig["severity"]
	if !ok {
		return defaultSeverity, nil
	}

	severity, ok := rawSeverity.(string)
	if !ok {
		return "", fmt.Errorf("invalid severity for rule %q: must be one of error, warning, info", ruleID)
	}

	severity = normalizeSeverity(severity)

	if _, ok := allowedSeverities[severity]; !ok {
		return "", fmt.Errorf("invalid severity for rule %q: %q (allowed: error, warning, info)", ruleID, severity)
	}

	return severity, nil
}

// EffectiveSeverity returns the configured severity for the given rule ID, or
// defaultSeverity when no valid override is present.
func EffectiveSeverity(ruleID string, cfg Config, defaultSeverity string) string {
	severity, err := resolveSeverity(ruleID, cfg, defaultSeverity)
	if err != nil {
		return defaultSeverity
	}
	return severity
}

// RuleEnabled reports whether a rule should run.
//
// Rules are enabled by default unless explicitly configured with
// {"enabled": false}.
func RuleEnabled(ruleID string, cfg Config) bool {
	ruleConfig, ok := cfg[ruleID]
	if !ok {
		return true
	}

	rawEnabled, ok := ruleConfig["enabled"]
	if !ok {
		return true
	}

	enabled, ok := rawEnabled.(bool)
	if !ok {
		return true
	}

	return enabled
}

func normalizeMetaKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "suppression", "suppress", "suppressions", "suppression-selectors", "suppression_selectors":
		return "suppression-selectors"
	default:
		return normalized
	}
}

// NormalizeConfig normalizes per-rule metadata keys and validates rule IDs.
func NormalizeConfig(cfg Config, knownRuleIDs map[string]struct{}) (Config, error) {
	if cfg == nil {
		return Config{}, nil
	}

	normalized := make(Config, len(cfg))
	for ruleID, rawRuleCfg := range cfg {
		canonicalRuleID := strings.TrimSpace(ruleID)
		if canonicalRuleID == "" {
			return nil, fmt.Errorf("invalid config: rule id cannot be empty")
		}

		if _, ok := knownRuleIDs[canonicalRuleID]; !ok {
			return nil, fmt.Errorf("unknown rule id %q in config", canonicalRuleID)
		}

		normalizedRuleCfg := make(map[string]interface{}, len(rawRuleCfg))
		for key, value := range rawRuleCfg {
			normalizedRuleCfg[normalizeMetaKey(key)] = value
		}

		normalized[canonicalRuleID] = normalizedRuleCfg
	}

	if err := ValidateConfig(normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}

// ValidateConfig validates lint configuration values shared across rules.
func ValidateConfig(cfg Config) error {
	for ruleID := range cfg {
		if _, err := resolveSeverity(ruleID, cfg, "warning"); err != nil {
			return err
		}

		ruleConfig := cfg[ruleID]
		if rawEnabled, ok := ruleConfig["enabled"]; ok {
			if _, ok := rawEnabled.(bool); !ok {
				return fmt.Errorf("invalid enabled flag for rule %q: must be a boolean", ruleID)
			}
		}

		if rawSelectors, ok := ruleConfig["suppression-selectors"]; ok {
			var selectors []string
			switch typedSelectors := rawSelectors.(type) {
			case []interface{}:
				selectors = make([]string, 0, len(typedSelectors))
				for _, selector := range typedSelectors {
					selectorValue, ok := selector.(string)
					if !ok {
						return fmt.Errorf("invalid suppression selectors for rule %q: must be an array of strings", ruleID)
					}
					selectors = append(selectors, selectorValue)
				}
			case []string:
				selectors = typedSelectors
			default:
				return fmt.Errorf("invalid suppression selectors for rule %q: must be an array of strings", ruleID)
			}

			for _, selector := range selectors {
				if _, ok := ParseSuppressionSelector(selector); !ok {
					return fmt.Errorf("invalid suppression selector for rule %q: %q (expected %s)", ruleID, selector, SuppressionSelectorPattern)
				}
			}
		}
	}
	return nil
}

// NodeSubgraphContext returns subgraph context for a node ID when available.
func NodeSubgraphContext(d *model.Diagram, nodeID string) *model.IssueContext {
	return NodeSubgraphContextForOccurrence(d, nodeID, 0)
}

// NodeSubgraphContextForOccurrence returns subgraph context for the Nth
// occurrence (zero-based) of a node ID across subgraph declarations.
func NodeSubgraphContextForOccurrence(d *model.Diagram, nodeID string, occurrence int) *model.IssueContext {
	if occurrence < 0 {
		return nil
	}

	seen := 0
	for _, subgraph := range d.Subgraphs {
		for _, id := range subgraph.Nodes {
			if id == nodeID {
				if seen == occurrence {
					return &model.IssueContext{SubgraphID: subgraph.ID, SubgraphLabel: subgraph.Label}
				}
				seen++
			}
		}
	}
	return nil
}

// DiagramFamilyRule is implemented by rules that only apply to specific
// Mermaid diagram families.
type DiagramFamilyRule interface {
	Rule
	Families() []model.DiagramFamily
}

// SupportsDiagramFamily reports whether r should run for the given family.
func SupportsDiagramFamily(r Rule, family model.DiagramFamily) bool {
	familyRule, ok := r.(DiagramFamilyRule)
	if !ok {
		return true
	}

	families := familyRule.Families()
	if len(families) == 0 {
		return true
	}

	for _, supported := range families {
		if supported == family {
			return true
		}
	}

	return false
}

// Rule is the interface every lint rule must implement.
type Rule interface {
	// ID returns the unique rule identifier (e.g. "no-duplicate-node-ids").
	ID() string
	// Run evaluates the diagram and returns any issues found.
	Run(d *model.Diagram, cfg Config) []model.Issue
}
