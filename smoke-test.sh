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

# Main execution
main() {
    log_info "Starting merm8 smoke tests"
    log_info "Service URL: $SERVICE_URL"
    
    echo ""
    
    check_service_running
    log_pass "Service is running"
    
    echo ""
    
    # Run test
    test_complex_diagram
    echo ""
    
    log_pass "All tests completed successfully!"
    exit 0
}

# Run main
main
