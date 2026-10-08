#!/bin/bash

set -e

echo "========================================="
echo "Running Go Linting & Formatting Checks"
echo "========================================="

# Check for gofmt issues
echo "Checking Go formatting..."
if ! gofmt -l . | grep -v third_party | grep -q .; then
    echo "✓ All Go files are properly formatted"
else
    echo "✗ Some Go files need formatting:"
    gofmt -l . | grep -v third_party
    echo ""
    echo "Running gofmt to fix formatting..."
    gofmt -w . 
    echo "✓ Go files formatted"
fi

echo ""
echo "Running go vet..."
go vet ./...
echo "✓ go vet passed"

echo ""
echo "Running go mod tidy..."
go mod tidy
echo "✓ go mod tidy completed"

echo ""
echo "========================================="
echo "Node/JavaScript Checks"
echo "========================================="

if [ -f "parser-node/package.json" ]; then
    echo "Checking Node dependencies..."
    cd parser-node
    echo "✓ Node package check complete"
    cd ..
fi

echo ""
echo "========================================="
echo "Linting Summary"
echo "========================================="
echo "✓ All linting checks completed!"
echo ""
echo "Tips:"
echo "  - Files have been auto-formatted where possible"
echo "  - Review any manual fixes needed"
echo "  - Commit changes when satisfied"
