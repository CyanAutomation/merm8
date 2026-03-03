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

# Check if service is running with retry logic
check_service_running() {
    local max_retries=3
    local delay=1
    
    for ((i=0; i<max_retries; i++)); do
        response=$(curl -s -w "\n%{http_code}" -X POST "$SERVICE_URL/analyze" \
            -H "Content-Type: application/json" \
            -d '{"code":""}')
        
        # Extract status code (last line)
        status_code=$(echo "$response" | tail -n1)
        
        # Accept 200 (success) or 400 (bad request - at least parsed)
        if [[ "$status_code" =~ ^[24][0-9][0-9]$ ]]; then
            return 0
        fi
        
        if [ $((i + 1)) -lt $max_retries ]; then
            log_info "Service not yet ready (retry $((i+1))/$((max_retries-1)))..."
            sleep $delay
        fi
    done
    
    log_fail "Service not available at $SERVICE_URL after $max_retries attempts"
}


# Test: Complex diagram with proper validation
test_complex_diagram() {
    log_info "Test: Complex diagram validation"
    
    # Using decision node and realistic flow
    code="graph TD
    A[Start] --> B[Process]
    B --> C{Decision}
    C -->|Yes| D[Output]
    C -->|No| E[Error]
    D --> F[End]"
    
    response=$(call_api "$code")
    
    # Validate diagram is correctly parsed
    valid=$(echo "$response" | jq '.valid')
    if [ "$valid" != "true" ]; then
        log_fail "Expected complex diagram to be valid, got: $(echo "$response" | jq -c .)"
    fi
    
    # Validate exact node and edge counts
    node_count=$(echo "$response" | jq '.metrics.node_count')
    if [ "$node_count" != "6" ]; then
        log_fail "Expected exactly 6 nodes (A,B,C,D,E,F), got $node_count"
    fi
    
    edge_count=$(echo "$response" | jq '.metrics.edge_count')
    if [ "$edge_count" != "5" ]; then
        log_fail "Expected exactly 5 edges, got $edge_count"
    fi
    
    log_pass "Complex diagram test passed"
}

# Test: SARIF endpoint - valid diagram with no issues
test_sarif_endpoint() {
    log_info "Test: SARIF endpoint with valid diagram"
    
    code="graph TD
    A[Start] --> B[End]"
    
    response=$(curl -s -X POST "$SERVICE_URL/analyze/sarif" \
        -H "Content-Type: application/json" \
        -d "{\"code\": $(echo "$code" | jq -R .)}")
    
    # Verify SARIF structure
    version=$(echo "$response" | jq '.version' 2>/dev/null)
    if [ "$version" != '"2.1.0"' ]; then
        log_fail "Expected SARIF version 2.1.0, got: $version"
    fi
    
    # Verify runs array exists
    runs_count=$(echo "$response" | jq '.runs | length' 2>/dev/null)
    if [ "$runs_count" -ne 1 ]; then
        log_fail "Expected 1 run, got: $runs_count"
    fi
    
    # Verify tool information
    tool_name=$(echo "$response" | jq -r '.runs[0].tool.driver.name' 2>/dev/null)
    if [ "$tool_name" != "merm8" ]; then
        log_fail "Expected tool name 'merm8', got: $tool_name"
    fi
    
    # Verify results array (should be empty for valid diagram with no issues)
    results_count=$(echo "$response" | jq '.runs[0].results | length' 2>/dev/null)
    if [ "$results_count" -ne 0 ]; then
        log_fail "Expected no results for clean diagram, got: $results_count"
    fi
    
    log_pass "SARIF endpoint test passed"
}

# Test: SARIF endpoint - diagram with issues
test_sarif_with_issues() {
    log_info "Test: SARIF endpoint with diagram containing issues"
    
    code="graph TD
    A[Start] --> B[Process]
    A --> C[Process]
    A --> D[Process]
    A --> E[Process]
    A --> F[Process]
    A --> G[Process]
    A --> H[Process]"
    
    # Use config to set max-fanout to 5, so A with 7 edges violates rule
    config='{"schema-version":"v1","rules":{"max-fanout":{"enabled":true,"limit":5,"severity":"error"}}}'
    
    response=$(curl -s -X POST "$SERVICE_URL/analyze/sarif" \
        -H "Content-Type: application/json" \
        -d "{\"code\": $(echo "$code" | jq -R .), \"config\": $config}")
    
    # Verify SARIF version
    version=$(echo "$response" | jq '.version' 2>/dev/null)
    if [ "$version" != '"2.1.0"' ]; then
        log_fail "Expected SARIF version 2.1.0, got: $version"
    fi
    
    # Verify results contain an error
    results_count=$(echo "$response" | jq '.runs[0].results | length' 2>/dev/null)
    if [ "$results_count" -eq 0 ]; then
        log_fail "Expected lint violations in SARIF results"
    fi
    
    # Verify error level is present
    error_level=$(echo "$response" | jq '.runs[0].results[0].level' 2>/dev/null)
    if [ "$error_level" != '"error"' ]; then
        log_fail "Expected error level in SARIF result, got: $error_level"
    fi
    
    # Verify ruleId is present
    rule_id=$(echo "$response" | jq -r '.runs[0].results[0].ruleId' 2>/dev/null)
    if [ "$rule_id" != "max-fanout" ]; then
        log_fail "Expected ruleId 'max-fanout', got: $rule_id"
    fi
    
    log_pass "SARIF with issues test passed"
}

# Helper: Make multiple concurrent requests
make_concurrent_requests() {
    local count=$1
    local url=$2
    local body=$3
    
    for ((i = 0; i < count; i++)); do
        curl -s -X POST "$url" \
            -H "Content-Type: application/json" \
            -d "$body" > /dev/null 2>&1 &
    done
    wait
}

# Test: Rate limiting (if configured)
test_rate_limiting() {
    # Skip if rate limiting not configured
    if [ -z "$TEST_RATE_LIMIT_ENABLED" ]; then
        log_info "Skipping rate limit test (TEST_RATE_LIMIT_ENABLED not set)"
        return
    fi
    
    log_info "Test: Rate limiting behavior"
    
    local limit=${TEST_RATE_LIMIT_PER_MIN:-5}
    local code='{"code":"graph TD\n  A[Start] --> B[End]"}'
    
    # Make requests equal to limit
    local passed=0
    for ((i = 0; i < limit; i++)); do
        status=$(curl -s -w "\n%{http_code}" -X POST "$SERVICE_URL/analyze" \
            -H "Content-Type: application/json" \
            -d "$code" | tail -n1)
        
        if [[ "$status" == "200" ]]; then
            ((passed++))
        fi
    done
    
    if [ "$passed" -ne "$limit" ]; then
        log_fail "Expected $limit successful requests, got $passed"
    fi
    
    # Next request should be rate limited (429)
    status=$(curl -s -w "\n%{http_code}" -X POST "$SERVICE_URL/analyze" \
        -H "Content-Type: application/json" \
        -d "$code" | tail -n1)
    
    if [ "$status" != "429" ]; then
        log_fail "Expected HTTP 429 (rate limited), got $status"
    fi
    
    log_pass "Rate limiting test passed"
}

# Main execution
main() {
    log_info "Starting merm8 smoke tests"
    log_info "Service URL: $SERVICE_URL"
    
    echo ""
    
    check_service_running
    log_pass "Service is running"
    
    echo ""
    
    # Run tests
    test_complex_diagram
    echo ""
    
    test_sarif_endpoint
    echo ""
    
    test_sarif_with_issues
    echo ""
    
    # Optional rate limiting test (can be enabled with TEST_RATE_LIMIT_ENABLED=1)
    if [ -n "$TEST_RATE_LIMIT_ENABLED" ]; then
        test_rate_limiting
        echo ""
    fi
    
    log_pass "All tests completed successfully!"
    exit 0
}

# Run main
main
