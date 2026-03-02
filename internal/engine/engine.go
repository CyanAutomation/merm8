// Package engine runs all registered rules against a Diagram and collects
// the resulting Issues.
package engine

import (
	"fmt"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// Engine holds a set of rules and runs them in order.
type Engine struct {
	rules []rules.Rule
}

// New returns an Engine pre-loaded with the default rule set.
func New() *Engine {
	return &Engine{
		rules: []rules.Rule{
			rules.NoDuplicateNodeIDs{},
			rules.NoDisconnectedNodes{},
			rules.MaxFanout{},
		},
	}
}

// NormalizeConfig validates and canonicalizes rule config keys to rule IDs.
// Unknown rule IDs are ignored and reported in returned warnings.
func (e *Engine) NormalizeConfig(cfg rules.Config) (rules.Config, []string) {
	normalized := make(rules.Config, len(cfg))
	if len(cfg) == 0 {
		return normalized, nil
	}

	canonical := make(map[string]string, len(e.rules))
	for _, r := range e.rules {
		id := r.ID()
		canonical[strings.ToLower(id)] = id
	}
		canonical[strings.ToLower(id)] = id
	}

	var warnings []string
	for rawKey, rc := range cfg {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			warnings = append(warnings, "ignored config for empty rule id")
			continue
		}
		if id, ok := canonical[strings.ToLower(key)]; ok {
			normalized[id] = rc
			continue
		}
		warnings = append(warnings, fmt.Sprintf("ignored config for unknown rule %q", rawKey))
	}

	return normalized, warnings
}

// Run executes every enabled rule against d and returns all issues found.
func (e *Engine) Run(d *model.Diagram, cfg rules.Config) []model.Issue {
	normalized, _ := e.NormalizeConfig(cfg)

	var issues []model.Issue
	for _, r := range e.rules {
		rc := normalized[r.ID()]
		if !rc.EnabledOrDefault() {
			continue
		}
		issues = append(issues, r.Run(d, normalized)...)
	}
	if issues == nil {
		issues = []model.Issue{}
	}
	return issues
}
