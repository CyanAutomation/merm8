package rules

import (
	"regexp"
	"strings"
)

// SuppressionSelectorPattern accepts canonical selectors in the form:
//   - node:<id>
//   - subgraph:<id>
//   - rule:<id>
//
// and the optional negated variants prefixed with '!'.
//
// Whitespace policy: selectors must not contain whitespace anywhere.
const SuppressionSelectorPattern = `^!?(node|subgraph|rule):\S+$`

var suppressionSelectorPatternRE = regexp.MustCompile(SuppressionSelectorPattern)

type SuppressionSelector struct {
	Negated bool
	Prefix  string
	Value   string
}

func ParseSuppressionSelector(raw string) (SuppressionSelector, bool) {
	if !suppressionSelectorPatternRE.MatchString(raw) {
		return SuppressionSelector{}, false
	}

	selector := raw
	negated := false
	if strings.HasPrefix(selector, "!") {
		negated = true
		selector = selector[1:]
	}

	prefix, value, _ := strings.Cut(selector, ":")

	return SuppressionSelector{Negated: negated, Prefix: prefix, Value: value}, true
}

// ValidateSuppressionSelectors validates that suppression selectors reference known rules.
// Returns a list of warnings for selectors that reference unknown rule IDs.
// Wildcard suppressions (rule:*) are always accepted.
func ValidateSuppressionSelectors(selectors []string, knownRuleIDs map[string]struct{}) []string {
	warnings := []string{}
	for _, selector := range selectors {
		parsed, ok := ParseSuppressionSelector(selector)
		if !ok {
			continue // Invalid format already handled by pattern validation elsewhere
		}
		
		// Only validate rule: prefix selectors
		if parsed.Prefix != "rule" {
			continue
		}
		
		// Allow wildcard suppressions
		if parsed.Value == "*" {
			continue
		}
		
		// Check if rule exists
		if _, exists := knownRuleIDs[parsed.Value]; !exists {
			warnings = append(warnings, "suppression selector references unknown rule: "+parsed.Value)
		}
	}
	return warnings
}
