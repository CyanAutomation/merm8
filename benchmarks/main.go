//go:build ignore
// +build ignore

// This file is used to run benchmarks from the command line.
// Usage: go run ./benchmarks/main.go [options]

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CyanAutomation/merm8/benchmarks"
)

func main() {
	var (
		ruleFilter          = flag.String("rule", "", "Run only cases for specific rule (e.g., 'no-cycles')")
		categoryFilter      = flag.String("category", "", "Filter by category: 'valid', 'violation', 'edge-case'")
		compareBaseline     = flag.String("compare-baseline", "", "Path to baseline JSON file to compare against")
		regressionThreshold = flag.Float64("regression-threshold", 5.0, "Regression detection threshold percentage (default: 5.0)")
		verbose             = flag.Bool("verbose", false, "Verbose output")
		help                = flag.Bool("help", false, "Show help message")
	)

	flag.Parse()

	if *help {
		fmt.Println(`merm8 Benchmark Runner

Usage: go run ./benchmarks/main.go [options]

Options:
  -rule string
        Run only cases for specific rule (e.g., 'no-cycles')
  -category string
        Filter by category: 'valid', 'violation', 'edge-case'
  -compare-baseline string
        Path to baseline JSON file to compare against (e.g., 'benchmarks/baselines/v0.1.0.json')
  -regression-threshold float
        Regression detection threshold percentage (default: 5.0)
  -verbose
        Verbose output
  -help
        Show help message

Examples:
  # Run all benchmarks
  go run ./benchmarks/main.go

  # Run only no-cycles rule benchmarks
  go run ./benchmarks/main.go -rule no-cycles

  # Run benchmarks and compare against baseline, alert on >2% regression
  go run ./benchmarks/main.go -compare-baseline benchmarks/baselines/v0.1.0.json -regression-threshold 2.0

  # Run verbose output with filter
  go run ./benchmarks/main.go -category violation -verbose
`)
		return
	}

	// Locate benchmark directory
	benchmarkDir := "benchmarks"
	if _, err := os.Stat(benchmarkDir); os.IsNotExist(err) {
		// Try relative to workspace
		cwd, _ := os.Getwd()
		benchmarkDir = filepath.Join(cwd, "benchmarks")
	}

	// Locate parser script
	parserScript := "parser-node/parse.mjs"
	if _, err := os.Stat(parserScript); os.IsNotExist(err) {
		parserScript = filepath.Join(os.Getenv("MERM8_PARSER_SCRIPT"), "parse.mjs")
		if _, err := os.Stat(parserScript); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: parser script not found. Set MERM8_PARSER_SCRIPT or ensure parser-node/parse.mjs exists\n")
			os.Exit(1)
		}
	}

	// Create runner
	runner := benchmarks.NewRunner(benchmarkDir, parserScript)

	// Run benchmarks
	opts := benchmarks.RunOptions{
		RuleFilter:          *ruleFilter,
		CategoryFilter:      *categoryFilter,
		Verbose:             *verbose,
		CompareTo:           *compareBaseline,
		RegressionThreshold: *regressionThreshold,
	}

	if err := runner.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
