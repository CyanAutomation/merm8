package api

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// assertPrometheusMetric validates a Prometheus metric line in the format:
// metricName{label1="value1",label2="value2",...} value
// Returns true if metric with expected labels and value is found.
func assertPrometheusMetric(t *testing.T, bodyStr, metricName string, expectedLabels map[string]string, expectedValue *float64) {
	t.Helper()

	// Build regex pattern: metricName{labels...} optionalValue
	// Example: request_total{route="/health",method="GET",status="200"} 1
	pattern := escapeMetricName(metricName) + `\{[^}]*\}`
	if expectedValue != nil {
		pattern += ` ` + regexp.QuoteMeta(strconv.FormatFloat(*expectedValue, 'f', -1, 64))
	}

	re := regexp.MustCompile(pattern)
	lines := strings.Split(bodyStr, "\n")

	for _, line := range lines {
		if !re.MatchString(line) {
			continue
		}

		// Check if labels match
		if labelsMatch(t, line, metricName, expectedLabels) {
			return // Found matching metric
		}
	}

	t.Fatalf("expected metric %s with labels %v, got body:\n%s", metricName, expectedLabels, bodyStr)
}

// labelsMatch extracts labels from a Prometheus metric line and compares with expected.
func labelsMatch(t *testing.T, line, metricName string, expectedLabels map[string]string) bool {
	// Extract label section: metricName{label1="val1",...}
	startIdx := strings.Index(line, "{")
	endIdx := strings.Index(line, "}")
	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return false
	}

	labelStr := line[startIdx+1 : endIdx]
	labels := parsePrometheusLabels(labelStr)

	// Check if all expected labels match
	for key, expectedVal := range expectedLabels {
		actualVal, ok := labels[key]
		if !ok || actualVal != expectedVal {
			return false
		}
	}
	return true
}

// parsePrometheusLabels parses Prometheus label string: label1="value1",label2="value2"
func parsePrometheusLabels(labelStr string) map[string]string {
	labels := make(map[string]string)
	// Split by comma, but be careful with quoted values
	pairs := strings.Split(labelStr, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		idx := strings.Index(pair, "=")
		if idx == -1 {
			continue
		}
		key := pair[:idx]
		val := pair[idx+1:]
		// Remove quotes from value
		val = strings.TrimPrefix(val, "\"")
		val = strings.TrimSuffix(val, "\"")
		labels[key] = val
	}
	return labels
}

// escapeMetricName escapes special regex chars in metric name.
func escapeMetricName(name string) string {
	return regexp.QuoteMeta(name)
}

// assertMetricLabelExists validates that a Prometheus metric with specific labels exists.
// Does not check value; only label presence and values.
func assertMetricLabelExists(t *testing.T, bodyStr, metricName string, expectedLabels map[string]string) {
	t.Helper()
	assertPrometheusMetric(t, bodyStr, metricName, expectedLabels, nil)
}

// assertMetricValue validates that a metric has a specific numeric value with given labels.
func assertMetricValue(t *testing.T, bodyStr, metricName string, expectedLabels map[string]string, expectedValue float64) {
	t.Helper()
	assertPrometheusMetric(t, bodyStr, metricName, expectedLabels, &expectedValue)
}

// Deprecated: Use assertMetricLabelExists instead.
// This function only checks substring presence in text, which is brittle.
// Kept for backwards compatibility during migration.
func assertPrometheusMetricStringContains(t *testing.T, bodyStr, expectedSubstring string) {
	t.Helper()
	if !strings.Contains(bodyStr, expectedSubstring) {
		t.Fatalf("expected prometheus output to contain %q, got body:\n%s", expectedSubstring, bodyStr)
	}
}
