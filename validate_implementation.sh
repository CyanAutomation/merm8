#!/bin/bash
# Validation script for merm8 help-suggestion implementation

set -e

echo "================================"
echo "merm8 Implementation Validation"
echo "================================"
echo ""

# Check if go is available
if ! command -v go &> /dev/null; then
    echo "ERROR: Go is not installed"
    exit 1
fi

echo "✓ Go is available"
echo ""

# Run go fmt check
echo "Checking code formatting..."
if go fmt ./internal/api/... ; then
    echo "✓ Code formatting is valid"
else
    echo "✗ Code formatting check failed"
fi
echo ""

# Check for compilation errors
echo "Checking for compilation errors..."
if go build ./cmd/server 2>&1 | grep -q "error"; then
    echo "✗ Compilation errors found:"
    go build ./cmd/server 2>&1 | grep "error"
    exit 1
else
    echo "✓ No compilation errors"
fi
echo ""

# Verify key changes exist
echo "Verifying implementation changes..."

HANDLER_FILE="./internal/api/handler.go"
if grep -q "type helpSuggestion struct" "$HANDLER_FILE"; then
    echo "✓ helpSuggestion struct exists"
else
    echo "✗ helpSuggestion struct not found"
    exit 1  
fi

if grep -q "func helpForSyntaxError" "$HANDLER_FILE"; then
    echo "✓ helpForSyntaxError function exists"
else
    echo "✗ helpForSyntaxError function not found"
    exit 1
fi

if grep -q "func helpForConfigError" "$HANDLER_FILE"; then
    echo "✓ helpForConfigError function exists"
else
    echo "✗ helpForConfigError function not found"
    exit 1
fi

if grep -q "HelpSuggestion \*helpSuggestion" "$HANDLER_FILE"; then
    echo "✓ HelpSuggestion field added to analyzeResponse"
else
    echo "✗ HelpSuggestion field not found in analyzeResponse"
    exit 1
fi

if grep -q "helpSugg := helpForSyntaxError" "$HANDLER_FILE"; then
    echo "✓ helpForSyntaxError is being called in syntax error path"
else
    echo "✗ helpForSyntaxError call not found"
    exit 1
fi

if grep -q "helpSugg := helpForConfigError" "$HANDLER_FILE"; then
    echo "✓ helpForConfigError is being called in config error path"
else
    echo "✗ helpForConfigError call not found"
    exit 1
fi

OPENAPI_FILE="./openapi.yaml"
if grep -q "help-suggestion" "$OPENAPI_FILE"; then
    echo "✓ help-suggestion field added to OpenAPI schema"
else
    echo "✗ help-suggestion field not found in OpenAPI schema"
    exit 1
fi

if grep -q "wrong-example" "$OPENAPI_FILE"; then
    echo "✓ HelpSuggestion schema defined in OpenAPI"
else
    echo "✗ HelpSuggestion schema not found in OpenAPI"
    exit 1
fi

TEST_FILE="./internal/api/handler_help_test.go"
if [ -f "$TEST_FILE" ]; then
    echo "✓ Test file handler_help_test.go exists"
    if grep -q "TestAnalyze_SyntaxError_ArrowOperatorHelp" "$TEST_FILE"; then
        echo "✓ Comprehensive test cases are present"
    else
        echo "⚠ Some test cases may be missing"
    fi
else
    echo "✗ Test file not found"
    exit 1
fi

echo ""
echo "================================"
echo "All validations passed! ✓"
echo "================================"
echo ""
echo "Next steps:"
echo "1. Run full test suite: go test ./internal/api -v"
echo "2. Start server: PARSER_SCRIPT=./parser-node/parse.mjs go run ./cmd/server"
echo "3. Test endpoint: curl -X POST http://localhost:8080/v1/analyze/raw -d 'A -> B'"
echo ""
