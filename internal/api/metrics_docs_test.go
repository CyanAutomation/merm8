package api_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMetricsDocsContainCurrentNames(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine caller path")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	docPath := filepath.Join(repoRoot, "docs", "metrics-observability.md")

	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read docs file: %v", err)
	}
	content := string(raw)

	mustContain := []string{
		"/metrics",
		"/internal/metrics",
		"request_total",
		"request_duration_seconds",
		"analyze_requests_total",
		"parser_duration_seconds",
		"route",
		"method",
		"status",
		"outcome",
		"valid_success",
		"syntax_error",
		"timeout",
		"subprocess",
		"decode",
		"contract",
		"internal",
	}

	for _, token := range mustContain {
		if !strings.Contains(content, token) {
			t.Fatalf("metrics docs missing token %q in %s", token, docPath)
		}
	}
}
