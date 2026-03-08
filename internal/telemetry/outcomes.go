package telemetry

import (
	"fmt"
)

// Outcome is a typed enumeration of analyze request outcomes for metrics.
// Using iota provides compile-time safety and prevents invalid outcome values.
type Outcome int

const (
	OutcomeSyntaxErrorType Outcome = iota
	OutcomeLintSuccessType
	OutcomeParserTimeoutType
	OutcomeParserSubprocessErrType
	OutcomeParserDecodeErrType
	OutcomeParserContractErrType
	OutcomeInternalErrorType
	OutcomeOtherType
)

// String returns the string representation of the outcome for Prometheus labels.
func (o Outcome) String() string {
	switch o {
	case OutcomeSyntaxErrorType:
		return "syntax_error"
	case OutcomeLintSuccessType:
		return "lint_success"
	case OutcomeParserTimeoutType:
		return "parser_timeout"
	case OutcomeParserSubprocessErrType:
		return "parser_subprocess_error"
	case OutcomeParserDecodeErrType:
		return "parser_decode_error"
	case OutcomeParserContractErrType:
		return "parser_contract_violation"
	case OutcomeInternalErrorType:
		return "internal_error"
	case OutcomeOtherType:
		return OutcomeOther
	default:
		return OutcomeOther
	}
}

// ValidOutcome checks if a string is a valid outcome value.
func ValidOutcome(s string) bool {
	switch s {
	case "syntax_error", "lint_success", "parser_timeout",
		"parser_subprocess_error", "parser_decode_error", "parser_contract_violation",
		"internal_error", OutcomeOther:
		return true
	default:
		return false
	}
}

// CanonicalOutcome coerces unsupported metric labels to a stable fallback value.
func CanonicalOutcome(s string) string {
	if ValidOutcome(s) {
		return s
	}
	return OutcomeOther
}

// OutcomeFromString converts a string to a typed Outcome.
// Returns an error if the string is not a valid outcome.
func OutcomeFromString(s string) (Outcome, error) {
	switch s {
	case "syntax_error":
		return OutcomeSyntaxErrorType, nil
	case "lint_success":
		return OutcomeLintSuccessType, nil
	case "parser_timeout":
		return OutcomeParserTimeoutType, nil
	case "parser_subprocess_error":
		return OutcomeParserSubprocessErrType, nil
	case "parser_decode_error":
		return OutcomeParserDecodeErrType, nil
	case "parser_contract_violation":
		return OutcomeParserContractErrType, nil
	case "internal_error":
		return OutcomeInternalErrorType, nil
	case OutcomeOther:
		return OutcomeOtherType, nil
	default:
		return -1, fmt.Errorf("invalid outcome: %s", s)
	}
}
