---
name: BenchmarkingAndPerformance
description: Measure and improve merm8 analysis performance with reproducible Mermaid fixtures and benchmark reports.
---

# Benchmarking and Performance

## Inputs / Outputs / Non-goals

- Inputs: `benchmarks/`, `docs/benchmarking.md`, `docs/parser-tuning.md`, `docs/metrics-observability.md`, and the code path under measurement.
- Outputs: A reproducible performance change or benchmark report with its workload and comparison basis stated.
- Non-goals: Do not treat a single noisy timing result as proof of improvement or add wall-clock assertions to unit tests.

## Trigger conditions

Use this skill when changing analysis throughput, parser latency, memory use, benchmark cases, baselines, or performance reporting.

## Mandatory rules

- Read the benchmark instructions and the implementation path first. Keep benchmark diagrams representative of the supported diagram families and rule workloads.
- Compare like-for-like runs: same benchmark cases, Go/Node versions, relevant configuration, and baseline format. Record changes to workload or methodology.
- Separate correctness/effectiveness measurements from runtime and memory measurements. Keep correctness checks deterministic and performance budgets out of ordinary unit tests unless explicitly designed as stable budgets.
- Change a baseline only when the measured result is understood and the baseline's scope/version is clear.
- For production tuning, correlate benchmark results with parser and service metrics; avoid extrapolating local microbenchmarks directly to deployment capacity.

## Validation checklist

- [ ] Run `make benchmark` for changes to benchmark execution or reports.
- [ ] Run targeted tests for benchmark fixtures, serialization, or comparison logic that changed.
- [ ] Compare against an existing compatible baseline and report workload, environment, and variance.
- [ ] Review generated reports and baseline diffs before including them.

## Expected output format

- Summary: Describe the performance behavior or benchmark artifact changed.
- Evidence: Report the workload, environment, baseline, and results.
- Risks: Note measurement limits, workload differences, or production assumptions.

## Failure/stop conditions

- If results are not comparable or too noisy to support a conclusion, report that and avoid claiming a measured improvement.
- Stop before changing production capacity settings based only on local benchmark data.
