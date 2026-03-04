package rules

import (
	"regexp"
	"strings"
)

const SuppressionSelectorPattern = `^!?(node|subgraph|rule):.+$`

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
