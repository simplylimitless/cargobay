#!/bin/bash

# Comprehensive Test Runner for Cargobay Backend
# Runs all test suites with coverage reporting

set -e

echo "========================================"
echo "Cargobay Backend - Test Suite Runner"
echo "========================================"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track pass/fail
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Function to run a test suite
run_suite() {
    local suite_name=$1
    local suite_path=$2

    echo -e "${YELLOW}Running: $suite_name${NC}"
    echo "----------------------------------------"

    # Run tests with coverage
    if go test -v -coverprofile="coverage_$suite_name.out" "$suite_path" 2>&1 | tee "test_output_$suite_name.txt"; then
        echo -e "${GREEN}✓ $suite_name PASSED${NC}"
        ((PASSED_TESTS++))
    else
        echo -e "${RED}✗ $suite_name FAILED${NC}"
        ((FAILED_TESTS++))
    fi

    echo ""
}

# Function to run a test package
run_package() {
    local package_name=$1
    local package_path=$2

    echo -e "${YELLOW}Running: $package_name${NC}"
    echo "----------------------------------------"

    # Run tests
    if go test -v "$package_path" 2>&1 | tee "test_output_$package_name.txt"; then
        echo -e "${GREEN}✓ $package_name PASSED${NC}"
        ((PASSED_TESTS++))
    else
        echo -e "${RED}✗ $package_name FAILED${NC}"
        ((FAILED_TESTS++))
    fi

    echo ""
}

# Change to backend directory
cd "$(dirname "$0")/.."

echo "Running tests from: $(pwd)"
echo ""

# Run unit tests for each package
echo "========================================"
echo "Running Unit Tests"
echo "========================================"
echo ""

# Storage tests
run_package "Storage" "./pkg/storage/..."

# Database tests
run_package "Database" "./pkg/database/..."

# RBAC tests
run_package "RBAC" "./pkg/rbac/..."

# Cache tests
run_package "Cache" "./pkg/cache/..."

# Vulnerability Scanner tests
run_package "Vulnerability Scanner" "./pkg/vulnerability/..."

echo "========================================"
echo "Running Integration Tests"
echo "========================================"
echo ""

# Integration tests
run_package "Integration" "./pkg/integration/..."

echo "========================================"
echo "API Tests"
echo "========================================"
echo ""

# API tests
run_package "API" "./pkg/api/..."

echo "========================================"
echo "Test Summary"
echo "========================================"
echo ""
echo "Passed: $PASSED_TESTS"
echo "Failed: $FAILED_TESTS"
echo ""

# Generate coverage report
echo "========================================"
echo "Coverage Report"
echo "========================================"
echo ""

# Collect coverage profiles
if ls coverage_*.out 2>/dev/null | head -1 > /dev/null; then
    go tool cover -func=coverage_*.out | grep -E "^(total|pkg/)" | sort -u
    echo ""

    # Generate HTML coverage report
    go tool cover -html=coverage_*.out -o coverage.html
    echo "HTML coverage report generated: coverage.html"
else
    echo "No coverage profiles found."
fi

# Cleanup temporary files
rm -f coverage_*.out test_output_*.txt

echo ""
echo "========================================"
if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some tests failed.${NC}"
    exit 1
fi
