package parser_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

func TestParserCache_CachesSuccessfulParses(t *testing.T) {
	t.Setenv("COUNTER_FILE", filepath.Join(t.TempDir(), "counter-success.log"))
	script, root := writeCacheTestParserScript(t)
	p := mustNewCacheTestParser(t, script, root)

	code := "graph TD\nA-->B"

	diagram1, syntaxErr1, err := p.Parse(code)
	if err != nil || syntaxErr1 != nil || diagram1 == nil {
		t.Fatalf("expected successful parse on first request, got diagram=%v syntax=%v err=%v", diagram1, syntaxErr1, err)
	}

	diagram2, syntaxErr2, err := p.Parse(code)
	if err != nil || syntaxErr2 != nil || diagram2 == nil {
		t.Fatalf("expected successful parse on cached request, got diagram=%v syntax=%v err=%v", diagram2, syntaxErr2, err)
	}
	if !reflect.DeepEqual(diagram2, diagram1) {
		t.Fatalf("cached response differs from original parse:\nfirst=%#v\nsecond=%#v", diagram1, diagram2)
	}

	counterContent, err := os.ReadFile(os.Getenv("COUNTER_FILE"))
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	if got := strings.Count(string(counterContent), "parse"); got != 1 {
		t.Fatalf("expected parser subprocess to run once, got %d invocations", got)
	}
}

func TestParserCache_DoesNotReuseResultAcrossTimeoutOverrides(t *testing.T) {
	t.Setenv("PARSER_MODE", "subprocess")
	t.Setenv("COUNTER_FILE", filepath.Join(t.TempDir(), "counter-timeout-override.log"))
	script, root := writeCacheTestParserScript(t)
	p := mustNewCacheTestParser(t, script, root)
	code := "graph TD\nA-->B"

	if diagram, syntaxErr, err := p.Parse(code); err != nil || syntaxErr != nil || diagram == nil {
		t.Fatalf("expected initial parse to succeed, got diagram=%v syntax=%v err=%v", diagram, syntaxErr, err)
	}

	t.Setenv("SLOW_PARSER", "1")
	config := parser.DefaultConfig()
	config.Timeout = 500 * time.Millisecond
	enhancementEnabled := true
	config.SourceEnhancement = &enhancementEnabled
	config.NeedSourceEnhancement = true
	_, _, err := p.ParseWithConfig(code, config)
	if !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected shorter timeout override to execute and time out, got %v", err)
	}
	assertFileLineCount(t, os.Getenv("COUNTER_FILE"), 2)
}

func TestParserCache_CachesSyntaxErrors(t *testing.T) {
	t.Setenv("COUNTER_FILE", filepath.Join(t.TempDir(), "counter-syntax.log"))
	script, root := writeCacheTestParserScript(t)
	p := mustNewCacheTestParser(t, script, root)

	code := "graph TD\nA-->"

	_, syntaxErr1, err := p.Parse(code)
	if err != nil || syntaxErr1 == nil {
		t.Fatalf("expected syntax error on first request, got syntax=%v err=%v", syntaxErr1, err)
	}
	_, syntaxErr2, err := p.Parse(code)
	if err != nil || syntaxErr2 == nil {
		t.Fatalf("expected syntax error on cached request, got syntax=%v err=%v", syntaxErr2, err)
	}
	if syntaxErr1.Message != syntaxErr2.Message || syntaxErr1.Line != syntaxErr2.Line || syntaxErr1.Column != syntaxErr2.Column {
		t.Fatalf("expected syntax errors to match between cached and uncached responses")
	}

	counterContent, err := os.ReadFile(os.Getenv("COUNTER_FILE"))
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	if got := strings.Count(string(counterContent), "parse"); got != 1 {
		t.Fatalf("expected parser subprocess to run once for syntax error, got %d invocations", got)
	}
}

func TestParserCache_DoesNotCacheInternalFailures(t *testing.T) {
	t.Setenv("COUNTER_FILE", filepath.Join(t.TempDir(), "counter-failure.log"))
	t.Setenv("FAIL_MARKER", filepath.Join(t.TempDir(), "failed-once.marker"))
	t.Setenv("FAIL_INTERNAL_ONCE", "1")
	script, root := writeCacheTestParserScript(t)
	p := mustNewCacheTestParser(t, script, root)

	_, _, err := p.Parse("graph TD\nA-->B")
	if !errors.Is(err, parser.ErrSubprocess) {
		t.Fatalf("expected subprocess error for transient internal failure, got %v", err)
	}

	diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
	if err != nil || syntaxErr != nil || diagram == nil {
		t.Fatalf("expected successful second parse after transient error, got diagram=%v syntax=%v err=%v", diagram, syntaxErr, err)
	}

	counterContent, err := os.ReadFile(os.Getenv("COUNTER_FILE"))
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	if got := strings.Count(string(counterContent), "parse"); got != 2 {
		t.Fatalf("expected parser subprocess to run twice because transient failures are uncached, got %d", got)
	}
}

func TestParserCache_DeduplicatesInFlightParses(t *testing.T) {
	t.Setenv("COUNTER_FILE", filepath.Join(t.TempDir(), "counter-inflight.log"))
	script, root := writeCacheTestParserScript(t)
	p := mustNewCacheTestParser(t, script, root)

	const workers = 8
	type parseResponse struct {
		diagram   *model.Diagram
		syntaxErr *parser.SyntaxError
		err       error
	}
	results := make([]parseResponse, workers)

	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait()
			diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
			results[idx] = parseResponse{diagram: diagram, syntaxErr: syntaxErr, err: err}
		}(i)
	}
	start.Done()
	wg.Wait()

	for i, result := range results {
		if result.err != nil || result.syntaxErr != nil || result.diagram == nil {
			t.Fatalf("expected successful parse result at index %d, got diagram=%v syntax=%v err=%v", i, result.diagram, result.syntaxErr, result.err)
		}
	}

	results[0].diagram.Nodes[0].ID = "mutated"
	if results[1].diagram.Nodes[0].ID != "a" {
		t.Fatalf("expected independent cloned diagrams across deduplicated fan-out, got %q", results[1].diagram.Nodes[0].ID)
	}

	counterContent, err := os.ReadFile(os.Getenv("COUNTER_FILE"))
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	if got := strings.Count(string(counterContent), "parse"); got != 1 {
		t.Fatalf("expected parser subprocess to run once for concurrent duplicate requests, got %d invocations", got)
	}
}

func mustNewCacheTestParser(t *testing.T, scriptPath, root string) *parser.Parser {
	t.Helper()
	t.Setenv("PARSER_MODE", "subprocess")
	p, err := parser.NewWithConfigAndRepoRootResolver(scriptPath, parser.Config{Timeout: 10 * time.Second}, func() (string, error) {
		return root, nil
	})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
	return p
}

func writeCacheTestParserScript(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module cachetest\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	t.Setenv("MERM8_TEST_PARSER_MODE", "cache")
	return writeMockParserFixture(t, tmpDir, "parse.ts"), tmpDir
}
