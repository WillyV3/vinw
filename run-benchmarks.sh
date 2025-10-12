#!/bin/bash

###############################################################################
# vinw Benchmark Test Suite
#
# Tests vinw performance across small/medium/large repositories
# Outputs timing data for comparison
###############################################################################

set -euo pipefail

SMALL_REPO="/Users/williamvansickleiii/Downloads/_REMIX_FR-END_MAIN_PG_/remix-webpack-demo.git"
MEDIUM_REPO="/Users/williamvansickleiii/Downloads/_REMIX_FR-END_MAIN_PG_/remix.git"
LARGE_REPO="/Users/williamvansickleiii/mycode/dify"

OUTPUT_FILE="${1:-MEASUREMENTS.txt}"

echo "🔍 Running vinw Performance Benchmarks" | tee "$OUTPUT_FILE"
echo "Date: $(date)" | tee -a "$OUTPUT_FILE"
echo "vinw version: $(git describe --tags 2>/dev/null || echo 'dev')" | tee -a "$OUTPUT_FILE"
echo "" | tee -a "$OUTPUT_FILE"

# Function to run vinw and extract timing
benchmark_repo() {
  local name=$1
  local path=$2

  if [ ! -d "$path" ]; then
    echo "⚠️  $name: Directory not found: $path" | tee -a "$OUTPUT_FILE"
    return
  fi

  local file_count=$(fd --type f . "$path" 2>/dev/null | wc -l | tr -d ' ')

  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" | tee -a "$OUTPUT_FILE"
  echo "Repository: $name" | tee -a "$OUTPUT_FILE"
  echo "Path: $path" | tee -a "$OUTPUT_FILE"
  echo "Files: $file_count" | tee -a "$OUTPUT_FILE"
  echo "" | tee -a "$OUTPUT_FILE"

  # Run vinw in test mode (needs to be added to vinw)
  # For now, just time the startup
  cd "$path"

  # Build vinw first
  if [ -f "/Users/williamvansickleiii/charmtuitemplate/vinw/app/vinw" ]; then
    VINW="/Users/williamvansickleiii/charmtuitemplate/vinw/app/vinw"
  else
    echo "Building vinw..." | tee -a "$OUTPUT_FILE"
    (cd /Users/williamvansickleiii/charmtuitemplate/vinw/app && go build -o vinw .) 2>&1 | tee -a "$OUTPUT_FILE"
    VINW="/Users/williamvansickleiii/charmtuitemplate/vinw/app/vinw"
  fi

  # Note: vinw is interactive, so we can't easily time it
  # This benchmark script is a placeholder for Phase 1 agent to enhance
  echo "Manual timing required (vinw is interactive TUI)" | tee -a "$OUTPUT_FILE"
  echo "Agent should add instrumentation to main.go" | tee -a "$OUTPUT_FILE"
  echo "" | tee -a "$OUTPUT_FILE"
}

# Run benchmarks
benchmark_repo "SMALL (50 files)" "$SMALL_REPO"
benchmark_repo "MEDIUM (1,000 files)" "$MEDIUM_REPO"
benchmark_repo "LARGE (5,000 files)" "$LARGE_REPO"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" | tee -a "$OUTPUT_FILE"
echo "" | tee -a "$OUTPUT_FILE"
echo "✓ Benchmark suite complete" | tee -a "$OUTPUT_FILE"
echo "Output saved to: $OUTPUT_FILE" | tee -a "$OUTPUT_FILE"
echo "" | tee -a "$OUTPUT_FILE"
echo "Next step: Phase 1 agent should add timing instrumentation" | tee -a "$OUTPUT_FILE"
echo "  - Time buildTreeWithMaps() in main.go" | tee -a "$OUTPUT_FILE"
echo "  - Time GetAllGitDiffs() in internal/github.go" | tee -a "$OUTPUT_FILE"
echo "  - Log timing to stderr for capture" | tee -a "$OUTPUT_FILE"
