package rules

import (
	"fmt"
	"regexp"
	"strings"
)

const legacyCustomRuleIDTransitionWindow = "v1.4.0"

var (
	ruleIDSegmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	builtInRuleIDSet     = buildBuiltInRuleIDSet()
)

func buildBuiltInRuleIDSet() map[string]struct{} {
	set := make(map[string]struct{}, len(builtInRuleMetadata))
	for _, metadata := range builtInRuleMetadata {
		set[metadata.ID] = struct{}{}
	}
	return set
}

// ValidateRegisteredRuleID validates namespace policy for registered rule IDs.
//
// Supported IDs:
//   - built-ins:            max-fanout (legacy), core/max-fanout
//   - external plugin rule: custom/<provider>/<id>
//
// Legacy unnamespaced custom IDs remain accepted during a transition window.
func ValidateRegisteredRuleID(rawRuleID string) (warning string, err error) {
	ruleID := strings.TrimSpace(rawRuleID)
	if ruleID == "" {
		return "", fmt.Errorf("rule id cannot be empty")
	}

	if strings.HasPrefix(ruleID, "core/") {
		ruleName := strings.TrimPrefix(ruleID, "core/")
		if !isValidRuleIDSegment(ruleName) {
			return "", fmt.Errorf("invalid rule id %q: core namespace must be core/<id> where <id> matches [a-z0-9][a-z0-9-]*", rawRuleID)
		}
		if _, isBuiltIn := builtInRuleIDSet[ruleName]; !isBuiltIn {
			return "", fmt.Errorf("invalid rule id %q: core/ namespace is reserved for built-in rules", rawRuleID)
		}
		return "", nil
	}

	if strings.HasPrefix(ruleID, "custom/") {
		parts := strings.Split(ruleID, "/")
		if len(parts) != 3 || !isValidRuleIDSegment(parts[1]) || !isValidRuleIDSegment(parts[2]) {
			return "", fmt.Errorf("invalid rule id %q: custom namespace must be custom/<provider>/<id>", rawRuleID)
		}
		return "", nil
	}

	if strings.Contains(ruleID, "/") {
		return "", fmt.Errorf("invalid rule id %q: unsupported namespace prefix", rawRuleID)
	}

	if !isValidRuleIDSegment(ruleID) {
		return "", fmt.Errorf("invalid rule id %q: id must match [a-z0-9][a-z0-9-]*", rawRuleID)
	}

	if _, isBuiltIn := builtInRuleIDSet[ruleID]; isBuiltIn {
		return "", nil
	}

	warning = fmt.Sprintf("legacy unnamespaced custom rule id %q is deprecated and will be removed in %s; migrate to custom/<provider>/%s", ruleID, legacyCustomRuleIDTransitionWindow, ruleID)
	return warning, nil
}

// CanonicalRuleRegistrationID normalizes a rule ID to canonical namespace form
// for collision detection during rule registration.
func CanonicalRuleRegistrationID(rawRuleID string) string {
	ruleID := strings.TrimSpace(rawRuleID)
	if strings.HasPrefix(ruleID, "core/") || strings.HasPrefix(ruleID, "custom/") {
		return ruleID
	}
	if _, isBuiltIn := builtInRuleIDSet[ruleID]; isBuiltIn {
		return "core/" + ruleID
	}
	return "custom/legacy/" + ruleID
}

// NormalizeConfigRuleID resolves config rule ID aliases against known rule IDs.
//
// Supported aliases:
//   - built-ins: max-fanout, core/max-fanout
//   - plugin IDs: exact ID match (for example custom/acme/check)
func NormalizeConfigRuleID(rawRuleID string, knownRuleIDs map[string]struct{}) (canonicalRuleID, warning string, err error) {
	ruleID := strings.TrimSpace(rawRuleID)
	if ruleID == "" {
		return "", "", fmt.Errorf("invalid config: rule id cannot be empty")
	}

	if strings.HasPrefix(ruleID, "core/") {
		innerRuleID := strings.TrimPrefix(ruleID, "core/")
		if !isValidRuleIDSegment(innerRuleID) {
			return "", "", fmt.Errorf("unknown rule id %q in config", ruleID)
		}
		if _, ok := builtInRuleIDSet[innerRuleID]; !ok {
			return "", "", fmt.Errorf("unknown rule id %q in config", ruleID)
		}
		if _, ok := knownRuleIDs[innerRuleID]; !ok {
			return "", "", fmt.Errorf("unknown rule id %q in config", ruleID)
		}
		return innerRuleID, "", nil
	}

	if _, ok := knownRuleIDs[ruleID]; ok {
		if _, builtIn := builtInRuleIDSet[ruleID]; builtIn {
			warning = fmt.Sprintf("legacy built-in rule id %q in config is accepted; prefer core/%s", ruleID, ruleID)
		}
		return ruleID, warning, nil
	}

	return "", "", fmt.Errorf("unknown rule id %q in config", ruleID)
}

func isValidRuleIDSegment(value string) bool {
	return ruleIDSegmentPattern.MatchString(value)
}
