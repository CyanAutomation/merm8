#!/bin/bash
set -e

echo "Running API handler tests..."
go test ./internal/api -count=1 -timeout 120s -v

echo ""
echo "Running parser tests..."
go test ./internal/parser -count=1 -timeout 60s -v

echo ""
echo "Running model tests..."
go test ./internal/model -count=1 -timeout 60s -v

echo ""
echo "Running rules tests..."
go test ./internal/rules -count=1 -timeout 60s -v

echo ""
echo "All tests completed successfully!"
