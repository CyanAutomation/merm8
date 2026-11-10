# Benchmark Suite — merm8 Mermaid Lint

The benchmark suite evaluates diagram analysis accuracy and performance across all rule families. Run it via the Go binary:

```bash
go run ./benchmarks/main.go -output json,html,csv,markdown
```

## Capabilities

| Feature | Description | Flag / Env |
| ------- | ----------- | ---------- |
| **False Positive Rate** | Reports `false_positive_rate` per rule (actual issues reported vs. total actual issues). Included in JSON, HTML, and CSV reports. | Always computed from fixture annotations |
| **CSV Output** | Exports results as comma-separated values for spreadsheet analysis and programmatic integration. | `--output csv` |
| **Comparison Baseline** | Detects performance regressions (`--compare-baseline <json-file>`): parse time increases >10%, lint time increases >10%. Alerts printed per affected rule with baseline-to-current values. | `--compare-baseline path.json` |
| **Coverage Analysis** | The HTML report includes a Coverage Analysis section showing full coverage status when all rules have ≥5 test cases; lists low-coverage rules (<5 cases) and uncovered diagram types. | Always shown in HTML output |
| **Sample Size Indicators** | Detection rate displays sample size (e.g., "100.00% (2/2)"). Rules with <5 test cases show a ⚠️ low-confidence badge. | Always computed |
| **Version Detection** | Three-tier detection: `MERM8_VERSION` env var (CI/build), `git describe --tags` (local dev), linker flags (`-ldflags`). Makefile propagates version via both env vars and linker flags. | Auto-detected; override with `MERM8_VERSION` |
| **Test Case Dedup Warning** | Discovery warns about duplicate fixture content (same `.mmd` file included multiple times under different names), identifying wasted benchmark runtime. | Logged at startup |
| **Markdown Output** | GitHub-flavored markdown export for CI/CD workflow comments. Written to `benchmarks/reports/latest-results.md`. Includes summary, rule metrics table, and failed cases. | `--output markdown` |
| **Trend Tracking** | Data structures track timestamped metric history per rule across runs (detection rate, false positive rate, parse/lint times). Stored as JSON-serializable structures for future longitudinal analysis. | Stored with results |
| **Parser Instance Caching** | Single parser instance initialized at startup and reused across all cases, eliminating subprocess creation overhead while maintaining identical result accuracy. | Internal optimization |
| **Rule-Specific Debug Context** | Failed test cases display contextual debugging guidance tied to each rule. Template maps rule IDs to actionable hints (e.g., `no-cycles`: "Verify no circular edges exist"). | Auto-applied in HTML reports |

## Fixture Format

Test fixtures use Mermaid comment annotations in the `.mmd` files under `benchmarks/cases/`:

- `%% @rule: no-cycles` — declares which rule the case exercises; if multiple comma-separated rule IDs are listed, the first is used
- `%% @count: N` — specifies the expected number of issues for a violation case (default: 1)
- Expected issues are matched against actual rule output to compute detection rate and false positive rate

Fixture directory structure:

```
benchmarks/cases/
  flowchart/          # 22 fixtures
    valid/  edge-cases/  violations/
  sequence/           # 17 fixtures
  class/              # 12 fixtures
  er/                 # 10 fixtures
  state/              # 11 fixtures
```

Total: 72 test cases across 5 diagram families. As new rules are registered, existing fixtures automatically evaluate against them.

## Report Formats

### JSON

Machine-readable output with full metric detail per rule, including pass/fail counts, detection rates, timing percentiles, and sample sizes.

### HTML

Interactive browser report with:
- Rule metrics table sortable by any column
- Search box filtering rules by name
- Click-to-sort headers with ↑/↓/↕ indicators
- False positive counts and rates in rule metrics
- Coverage Analysis section
- Version metadata header
- Vanilla JavaScript, zero dependencies, works offline

### CSV

Spreadsheet-compatible output for external analysis tools or CI dashboards. One row per rule with columns for pass count, detection rate, sample size, false positive rate, parse time, and lint time.

### Markdown

GitHub-flavored markdown suitable for CI/CD workflow comments. Pipe-delimited tables for readability in GitHub Flavored Markdown viewers. Written to `benchmarks/reports/latest-results.md`.

## Integration

### CI/CD Example (GitHub Actions)

```yaml
- name: Run benchmark suite
  run: |
    go run ./benchmarks/main.go \
      -output json,markdown \
      -compare-baseline benchmarks/baselines/v0.1.0.json || true
- name: Post results to PR
  if: github.event_name == 'pull_request'
  run: |
    cat benchmarks/reports/latest-results.md >> "$GITHUB_STEP_SUMMARY"
```

### Baseline Comparison Workflow

1. Save a known-good run's JSON report as a baseline (e.g., copy `benchmarks/reports/latest-results.json` to `benchmarks/baselines/<version>.json`)
2. Compare subsequent runs: `--compare-baseline benchmarks/baselines/<version>.json`
3. Review regression alerts for parse time >10% or lint time >10% increases
4. Update baseline when regression is intentional (e.g., after adding new rules)
