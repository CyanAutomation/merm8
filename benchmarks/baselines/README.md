# Benchmark Baselines

This directory stores baseline results for merm8 benchmark suites across different versions. Baselines are used for regression detection and trend analysis.

## Files

- **v0.1.0.json** — Initial baseline established on 2026-03-06
  - 15 total benchmark cases discovered automatically
  - 5 rules tested: no-duplicate-node-ids, no-disconnected-nodes, max-fanout, no-cycles, max-depth
  - Expected overall pass rate: **>85%**
  - All rules expected to have detection rate **≥90%**

## Interpreting Baselines

Each baseline file is a `BenchmarkResults` JSON object containing:

```json
{
  "timestamp": "ISO 8601",
  "version": "v0.1.0",
  "execution_time_ms": 5000,
  "total_cases": 15,
  "total_passed": 13,
  "rule_metrics": {
    "rule-id": {
      "total_cases": 3,
      "passed": 3,
      "detection_rate": 1.0,
      "false_positives": 0,
      "false_positive_rate": 0.0,
      ...
    }
  },
  "failed_cases": [...]
}
```

### Key Metrics

- **detection_rate** — Fraction of cases where the rule performed as expected (0–1)
- **false_positive_rate** — Fraction of reported issues that weren't expected
- **total_cases** — Number of test cases for this rule
- **avg_parse_time_ms** — Average milliseconds to parse fixtures
- **avg_lint_time_ms** — Average milliseconds to execute rule

## Using Baselines for Regression Detection

When running benchmarks:

```bash
go run ./benchmarks/main.go --compare-baseline benchmarks/baselines/v0.1.0.json
```

The runner will report any detection rate drop >5% (configurable):

```
⚠️  no-cycles: Detection rate dropped from 100.00% to 90.00% (10.00% drop, threshold: 5.00%)
```

## Creating a New Baseline

> Historical note: if older automation still points to `benchmarks/reports/index.html`, migrate to `benchmark.html`. The legacy path may be retained temporarily for compatibility.

After significant changes to rules or test fixtures:

```bash
# 1. Run benchmarks (this generates latest-results.json)
go run ./benchmarks/main.go

# 2. Review results in benchmark.html
# 3. If acceptable, snapshot as new baseline
cp benchmarks/reports/latest-results.json benchmarks/baselines/v0.1.1.json

# 4. Update file to have proper version/timestamp
# 5. Commit to git
git add benchmarks/baselines/v0.1.1.json
git commit -m "benchmark: establish v0.1.1 baseline"
```

## Baseline Best Practices

1. **Establish baselines for significant milestones**, not every run
2. **Review baseline results** for >85% pass rate before committing
3. **Document any deviations** in the commit message or CHANGELOG
4. **Update baseline comparisons** in CI workflows when new baseline is ready
5. **Archive old baselines** when no longer needed (but keep them for trend analysis)

## Trends and Analysis

Over time, baselines should show:

- **Increasing detection rates** (rules becoming more accurate)
- **Decreasing false positives** (rules becoming more precise)
- **Stable or improving execution times** (rules not regressing in performance)

If trends go in the opposite direction, investigate:

- Recent code changes
- New test cases added
- Parser or engine changes

## See Also

- [../BENCHMARK.md](../BENCHMARK.md) — Benchmark framework documentation
- [../CONTRIBUTING.md](../CONTRIBUTING.md) — How to add test cases
