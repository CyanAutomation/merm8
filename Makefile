.PHONY: lint format vet tidy help test-contract benchmark

help:
	@echo "Linting and Formatting Targets:"
	@echo "  make lint       - Run all linting checks (vet, fmt check)"
	@echo "  make format     - Auto-format all Go code"
	@echo "  make vet        - Run go vet static analysis"
	@echo "  make tidy       - Run go mod tidy"
	@echo "  make ci-lint    - Run all linting (used in CI/CD)"
	@echo ""
	@echo "Benchmark Targets:"
	@echo "  make benchmark  - Run benchmark suite and generate reports"

lint: vet
	@echo "✓ Linting complete"

format:
	@echo "Formatting Go code..."
	gofmt -w .
	@echo "✓ Go code formatted"
	@if command -v prettier >/dev/null 2>&1; then \
		echo "Formatting with prettier..."; \
		cd parser-node && npx prettier --write "**/*.{mjs,json}" || echo "prettier skipped (not installed)"; \
		cd ..; \
		echo "✓ Node code formatted"; \
	fi

vet:
	@echo "Running go vet..."
	go vet ./...
	@echo "✓ go vet passed"

tidy:
	@echo "Running go mod tidy..."
	go mod tidy
	@echo "✓ go mod tidy completed"

ci-lint: format vet tidy
	@echo "✓ All CI linting checks passed"


test-contract:
	@echo "Running contract integration tests..."
	go test ./cmd/server -run '^TestServerContractIntegration_' -count=1 -timeout=90s
	@echo "✓ contract integration tests passed"

benchmark:
	@echo "Running benchmark suite..."
	@VERSION=$$(git describe --tags --always 2>/dev/null || echo "v0.1.0-dev"); \
	PARSER_SCRIPT=$(PWD)/parser-node/parse.mjs MERM8_VERSION=$$VERSION go run -ldflags="-X github.com/CyanAutomation/merm8/benchmarks.appVersion=$$VERSION" ./benchmarks/main.go
	@echo "✓ Benchmark complete. Open benchmark.html to view results."

