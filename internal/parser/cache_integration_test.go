package parser_test

import (
	"errors"
	"os"
	"path/filepath"
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

	start := time.Now()
	diagram1, syntaxErr1, err := p.Parse(code)
	firstDuration := time.Since(start)
	if err != nil || syntaxErr1 != nil || diagram1 == nil {
		t.Fatalf("expected successful parse on first request, got diagram=%v syntax=%v err=%v", diagram1, syntaxErr1, err)
	}

	start = time.Now()
	diagram2, syntaxErr2, err := p.Parse(code)
	secondDuration := time.Since(start)
	if err != nil || syntaxErr2 != nil || diagram2 == nil {
		t.Fatalf("expected successful parse on cached request, got diagram=%v syntax=%v err=%v", diagram2, syntaxErr2, err)
	}
	if diagram2.Type != diagram1.Type || len(diagram2.Nodes) != len(diagram1.Nodes) || len(diagram2.Edges) != len(diagram1.Edges) {
		t.Fatalf("expected cached response to preserve correctness")
	}
	if secondDuration >= firstDuration/2 {
		t.Fatalf("expected second parse to be faster due to cache, first=%s second=%s", firstDuration, secondDuration)
	}

	counterContent, err := os.ReadFile(os.Getenv("COUNTER_FILE"))
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	if got := strings.Count(string(counterContent), "parse"); got != 1 {
		t.Fatalf("expected parser subprocess to run once, got %d invocations", got)
	}
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
	p, err := parser.NewWithConfigAndRepoRootResolver(scriptPath, parser.Config{}, func() (string, error) {
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

	scriptPath := filepath.Join(tmpDir, "parse.mjs")
	script := `import fs from "node:fs";
const counterFile = process.env.COUNTER_FILE || "";
if (process.argv.includes("--version-info")) {
  process.stdout.write(JSON.stringify({parser_version:"cache-test-v1",mermaid_version:"11.12.0"}));
  process.exit(0);
}
if (counterFile) {
  fs.appendFileSync(counterFile, "parse\n");
}
const failMarker = process.env.FAIL_MARKER || "";
if (process.env.FAIL_INTERNAL_ONCE === "1" && failMarker && !fs.existsSync(failMarker)) {
  fs.writeFileSync(failMarker, "done");
  process.stdout.write(JSON.stringify({valid:false,error:{message:"internal parser error: simulated",line:1,column:1}}));
  process.stderr.write("simulated internal failure");
  process.exit(1);
}
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", c => { input += c; });
process.stdin.on("end", () => {
  setTimeout(() => {
    if (input.includes("-->") && input.trim().endsWith("-->")) {
      process.stdout.write(JSON.stringify({valid:false,error:{message:"Syntax error",line:2,column:4}}));
      return;
    }
    process.stdout.write(JSON.stringify({
      valid:true,
      diagram_type:"flowchart",
      ast:{
        type:"flowchart",
        direction:"TD",
        nodes:[{id:"a",label:"A"},{id:"b",label:"B"}],
        edges:[{from:"a",to:"b",type:"-->",line:2,column:1}],
        subgraphs:[],
        suppressions:[]
      }
    }));
  }, 120);
});
process.stdin.resume();
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}
	return scriptPath, tmpDir
}
