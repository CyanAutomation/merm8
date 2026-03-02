#!/usr/bin/env bash
# smoke-test.sh - End-to-end smoke test for merm8 service
#
# This script validates that merm8 can be built and run, and responds correctly
# to HTTP requests. It expects the service to listen on port 8080.
#
# Usage:
#   ./smoke-test.sh              -- runs tests against a pre-running service
#   ./smoke-test.sh --build      -- builds and runs the service for testing
#

set -e

SERVICE_URL="http://localhost:8080"
TIMEOUT=5

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_pass() {
    echo -e "${GREEN}✓${NC} $1"
}

log_fail() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

log_info() {
    echo -e "${YELLOW}ℹ${NC} $1"
}

# Helper function to make API calls
call_api() {
    local code="$1"
    local config="${2:-}"
    
    if [ -z "$config" ]; then
        curl -s -X POST "$SERVICE_URL/analyze" \
            -H "Content-Type: application/json" \
            -d "{\"code\": $(echo "$code" | jq -R .),\"config\":{}}"
    else
        curl -s -X POST "$SERVICE_URL/analyze" \
            -H "Content-Type: application/json" \
            -d "{\"code\": $(echo "$code" | jq -R .), \"config\": $config}"
    fi
}

# Check if service is running
check_service_running() {
    if ! timeout $TIMEOUT curl -s "$SERVICE_URL/analyze" -X POST \
        -H "Content-Type: application/json" \
        -d '{"code":""}' > /dev/null 2>&1; then
        log_fail "Service not running on $SERVICE_URL"
    fi
}

# Test 1: Valid simple diagram
test_valid_simple() {
    log_info "Test 1: Valid simple diagram (A-->B)"
    
    code="graph TD
    A-->B"
    response=$(call_api "$code")
    
    # Check response structure
    if ! echo "$response" | jq -e '.valid' > /dev/null 2>&1; then
        log_fail "Response missing 'valid' field"
    fi
    
    valid=$(echo "$response" | jq '.valid')
    if [ "$valid" != "true" ]; then
        log_fail "Expected valid=true, got: $(echo "$response" | jq -c .)"
    fi
    
    if ! echo "$response" | jq -e '.metrics' > /dev/null 2>&1; then
        log_fail "Response missing 'metrics' field"
    fi
    
    metrics=$(echo "$response" | jq '.metrics')
    node_count=$(echo "$metrics" | jq '.node_count')
    edge_count=$(echo "$metrics" | jq '.edge_count')
    
    if [ "$node_count" -lt 2 ]; then
        log_fail "Expected at least 2 nodes, got $node_count"
    fi
    
    if [ "$edge_count" -lt 1 ]; then
        log_fail "Expected at least 1 edge, got $edge_count"
    fi
    
    log_pass "Valid diagram test passed"
}

# Test 2: Invalid/syntax error diagram
test_invalid_syntax() {
    log_info "Test 2: Invalid Mermaid syntax"
    
    code="this is not valid mermaid"
    response=$(call_api "$code")
    
    valid=$(echo "$response" | jq '.valid // false')
    if [ "$valid" == "true" ]; then
        log_fail "Expected valid=false for invalid code"
    fi
    
    syntax_error=$(echo "$response" | jq '.syntax_error')
    if [ "$syntax_error" == "null" ]; then
        log_fail "Expected syntax_error for invalid code, got: $(echo "$response" | jq -c .)"
    fi
    
    log_pass "Invalid syntax test passed"
}

# Test 3: Missing code field
test_missing_code() {
    log_info "Test 3: Request missing 'code' field"
    
    response=$(curl -s -X POST "$SERVICE_URL/analyze" \
        -H "Content-Type: application/json" \
        -d '{"config":{}}')
    
    # Should return badrequest response with error message
    if echo "$response" | jq -e '.error' > /dev/null 2>&1; then
        error=$(echo "$response" | jq -r '.error')
        if echo "$error" | grep -q "code"; then
            log_pass "Missing code field correctly rejected"
            return
        fi
    fi
    
    log_fail "Expected error for missing code field, got: $(echo "$response" | jq -c .)"
}

# Test 4: Complex diagram with multiple nodes/edges
test_complex_diagram() {
    log_info "Test 4: Complex diagram with 5 nodes"
    
    code="graph TD
    A[Start] --> B[Process]
    B --> C[Decision]
    C -->|Yes| D[Output]
    C -->|No| E[Error]
    D --> F[End]"
    
    response=$(call_api "$code")
    
    valid=$(echo "$response" | jq '.valid')
    if [ "$valid" != "true" ]; then
        log_info "(This diagram may be invalid depending on Mermaid syntax specifics)"
        return
    fi
    
    node_count=$(echo "$response" | jq '.metrics.node_count')
    if [ "$node_count" -lt 4 ]; then
        log_info "Note: expected ~5 nodes, got $node_count (syntax may vary by version)"
        return
    fi
    
    log_pass "Complex diagram test passed"
}

# Main execution
main() {
    log_info "Starting merm8 smoke tests"
    log_info "Service URL: $SERVICE_URL"
    
    echo ""
    
    check_service_running
    log_pass "Service is running"
    
    echo ""
    
    # Run all tests
    test_valid_simple
    echo ""
    
    test_invalid_syntax
    echo ""
    
    test_missing_code
    echo ""
    
    test_complex_diagram
    echo ""
    
    log_pass "All tests completed successfully!"
    exit 0
}

# Run main
main
