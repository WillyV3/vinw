#!/bin/bash

# vinw Test Suite
# Run various tests to ensure vinw is working correctly

set -e

echo "vinw Test Suite"
echo "==============="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Test function
test_case() {
    local name="$1"
    local cmd="$2"
    echo -n "Testing: $name... "
    if eval "$cmd" > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
    else
        echo -e "${RED}✗${NC}"
        echo "  Command: $cmd"
    fi
}

# Build tests
echo "Build Tests:"
test_case "vinw builds" "go build -o vinw"
test_case "viewer builds" "cd viewer && go build -o vinw-viewer && cd .."

# Skate tests
echo ""
echo "Skate Integration Tests:"
test_case "skate is installed" "which skate"
test_case "skate can write" "skate set test-key test-value"
test_case "skate can read" "skate get test-key | grep -q test-value"
test_case "skate can delete" "skate delete test-key"

# Git tests
echo ""
echo "Git Integration Tests:"
test_case "git is installed" "which git"
test_case "current dir is git repo" "git rev-parse --git-dir"
test_case "can get git diff" "git diff --numstat"
test_case "can get staged diff" "git diff --cached --numstat"

# File system tests
echo ""
echo "File System Tests:"
test_case ".gitignore exists" "test -f .gitignore"
test_case "can read directory" "ls -la > /dev/null"
test_case "go.mod exists" "test -f go.mod"
test_case "viewer go.mod exists" "test -f viewer/go.mod"

# GitHub CLI tests (optional)
echo ""
echo "GitHub CLI Tests (optional):"
if which gh > /dev/null 2>&1; then
    test_case "gh is installed" "which gh"
    test_case "gh is authenticated" "gh auth status"
else
    echo "GitHub CLI not installed (optional)"
fi

echo ""
echo "Test suite complete!"