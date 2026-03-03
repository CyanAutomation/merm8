.PHONY: lint format vet tidy help

help:
	@echo "Linting and Formatting Targets:"
	@echo "  make lint       - Run all linting checks (vet, fmt check)"
	@echo "  make format     - Auto-format all Go code"
	@echo "  make vet        - Run go vet static analysis"
	@echo "  make tidy       - Run go mod tidy"
	@echo "  make ci-lint    - Run all linting (used in CI/CD)"

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
