package sarif

import "strings"

const (
	SARIFLevelError   = "error"
	SARIFLevelWarning = "warning"
	SARIFLevelNote    = "note"
)

// SeverityMappings documents the canonical translation from merm8 issue
// severities to SARIF result levels.
var SeverityMappings = map[string]string{
	"error":   SARIFLevelError,
	"warning": SARIFLevelWarning,
	"warn":    SARIFLevelWarning,
	"info":    SARIFLevelNote,
}

const SeverityMappingDoc = "Severity mapping to SARIF level: error->error, warning/warn->warning, info->note."

// MapSeverityToLevel maps merm8 severities to SARIF result levels.
func MapSeverityToLevel(severity string) string {
	if level, ok := SeverityMappings[strings.ToLower(strings.TrimSpace(severity))]; ok {
		return level
	}
	return SARIFLevelWarning
}
