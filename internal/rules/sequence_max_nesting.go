package rules

import (
	"fmt"
	"math"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultSequenceMaxNesting = 3

// SequenceMaxNesting flags sequence diagrams with deeply nested blocks
// (rect, loop, alt, opt, etc.) that exceed the configured limit.
type SequenceMaxNesting struct{}

func (r SequenceMaxNesting) ID() string { return "max-nesting-depth" }

func (r SequenceMaxNesting) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilySequence}
}

func (r SequenceMaxNesting) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "warning")

	limit := defaultSequenceMaxNesting
	if rc, ok := cfg[r.ID()]; ok {
		if v, ok := rc["limit"]; ok {
			switch n := v.(type) {
			case int:
				if n >= 1 {
					limit = n
				}
			case float64:
				if n >= 1 && n == math.Trunc(n) {
					limit = int(n)
				}
			}
		}
	}

	// For now, we don't have nesting info from the parser for sequence diagrams.
	// This rule is a placeholder that will be enhanced when parser support is added.
	// Return no issues for now.
	_ = limit
	_ = severity

	return nil
}

// SequenceMaxNestingMessage is a placeholder for future implementation.
func SequenceMaxNestingMessage(depth, limit int) string {
	return fmt.Sprintf("nesting depth %d exceeds limit %d", depth, limit)
}
