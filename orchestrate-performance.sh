#!/bin/bash

###############################################################################
# vinw Performance Improvement Orchestration
#
# Real-world test of Gummy Agents pattern!
# Based on: /Users/williamvansickleiii/charmtuitemplate/vinw/app/PERFORMANCE_PLAN.md
#
# Philosophy: Unix principles - simple, modular, composable
# Agents work sequentially with human supervision at quality gates
###############################################################################

set -euo pipefail

###############################################################################
# CONFIGURATION
###############################################################################

PIPELINE_NAME="vinw-performance"
PIPELINE_DESC="vinw Performance Improvement - 4 Phase Plan"

# Define agents based on PERFORMANCE_PLAN.md phases
AGENTS=(
  "phase1-measurement:Phase 1: Add timing measurements to current code. Add timing logs to buildTreeWithMaps() and GetAllGitDiffs(). Test with small/medium/large repos. Output timing data to MEASUREMENTS.txt. Do NOT change any logic yet - just measure. Success: MEASUREMENTS.txt exists with timing data from at least 3 different repo sizes. Reference: @PERFORMANCE_PLAN.md lines 413-443."

  "phase2-extract:Phase 2: Extract pure functions without changing behavior. Create internal/fsscan.go with ScanFilesystem(). Create internal/filters.go with ApplyGitignore, ApplyHiddenFilter, ApplyNesting. Update main.go to use new functions. Success: go build ./... succeeds, all existing functionality works, code is more modular. Reference: @PERFORMANCE_PLAN.md lines 445-489."

  "phase3-optimize:Phase 3: Optimize the separated modules. Replace os.ReadDir with filepath.WalkDir in fsscan.go. Remove line counting from git diff (mark untracked as -1). Add 'r' and 'R' keybindings for manual refresh. Remove/extend auto-tick to 60s. Success: go build ./... && go test ./... pass, toggles feel instant. Reference: @PERFORMANCE_PLAN.md lines 491-557."

  "phase4-remeasure:Phase 4: Measure again and document improvements. Run same benchmarks as Phase 1. Compare results. Document speed improvements in IMPROVEMENTS.txt. Success: IMPROVEMENTS.txt shows quantified improvements (e.g., 'Tree rebuild: 40% faster'). Reference: @PERFORMANCE_PLAN.md lines 559-569."
)

# Go-specific quality gate
QUALITY_GATE="go build ./... && go test ./..."

###############################################################################
# INTERNAL VARIABLES
###############################################################################

LOG_DIR=".orchestration-logs"
STATE_FILE=".orchestrate-state.json"
START_TIME=$(date +%s)
TOTAL_COST=0
COMPLETED_AGENTS=()
CURRENT_AGENT=""

mkdir -p "$LOG_DIR"

###############################################################################
# UI FUNCTIONS
###############################################################################

show_header() {
  clear
  gum style \
    --border double \
    --border-foreground 212 \
    --align center \
    --width 70 \
    --margin "1 0" \
    "🍬 vinw Performance Orchestration"

  echo
  gum style --foreground 99 "Project: vinw TUI file manager"
  gum style --foreground 99 "Plan: 4-phase performance improvement"
  gum style --foreground 99 "Agents: ${#AGENTS[@]} sequential phases"
  gum style --foreground 240 "Logs: $LOG_DIR/"
  echo
}

show_agent_header() {
  local index=$1
  local total=$2
  local agent_name=$3

  echo
  gum style --foreground 212 "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  gum style --foreground 212 --bold "Phase ${index}/${total}: $agent_name"
  gum style --foreground 212 "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo
}

show_completion() {
  local elapsed=$(($(date +%s) - START_TIME))
  local minutes=$((elapsed / 60))

  echo
  gum style \
    --border double \
    --border-foreground 42 \
    --align center \
    --width 70 \
    --padding "1 2" \
    "🎉 Performance Improvements Complete!"

  echo
  gum style --foreground 42 "✓ All 4 phases completed successfully"
  gum style --foreground 99 "Total time: ${minutes} minutes"
  gum style --foreground 99 "Total cost: \$${TOTAL_COST}"
  echo
  gum style --foreground 240 "Results:"
  gum style --foreground 240 "  - MEASUREMENTS.txt (baseline)"
  gum style --foreground 240 "  - IMPROVEMENTS.txt (results)"
  gum style --foreground 240 "  - Logs: $LOG_DIR/"
  echo
}

###############################################################################
# AGENT EXECUTION
###############################################################################

spawn_agent() {
  local agent_name=$1
  local task=$2
  local log_file="$LOG_DIR/${agent_name}.log"

  gum style \
    --border rounded \
    --border-foreground 33 \
    --padding "1 2" \
    --margin "1 0" \
    "Task: $task"

  echo

  if ! gum confirm "Spawn $agent_name agent?"; then
    gum style --foreground 99 "⏭ Skipped $agent_name"
    return 1
  fi

  gum style --foreground 99 "⠋ Spawning agent..."

  # Key: Using claude --print to spawn autonomous agent
  claude --print "$task" \
    --output-format stream-json \
    --verbose \
    > "$log_file" 2>&1 &

  local pid=$!

  # Monitor with spinner
  gum spin \
    --spinner dot \
    --title "Agent $agent_name running..." \
    -- bash -c "while kill -0 $pid 2>/dev/null; do sleep 1; done"

  wait $pid
  local exit_code=$?

  if [ $exit_code -eq 0 ]; then
    local turns=$(grep -c '"type":"' "$log_file" 2>/dev/null || echo "?")
    local cost=$(jq -r 'select(.cost_usd) | .cost_usd' "$log_file" 2>/dev/null | \
      awk '{sum+=$1} END {printf "%.2f", sum}')
    cost=${cost:-0.00}

    TOTAL_COST=$(awk "BEGIN {printf \"%.2f\", $TOTAL_COST + $cost}")

    gum style --foreground 42 "✓ $agent_name completed (${turns} turns, \$${cost})"
    return 0
  else
    gum style --foreground 196 "✗ $agent_name failed"
    return 1
  fi
}

###############################################################################
# QUALITY GATE
###############################################################################

run_quality_gate() {
  local agent_name=$1

  echo
  gum style --foreground 99 "Running quality gate: $QUALITY_GATE"

  if gum spin --title "Building..." -- bash -c "$QUALITY_GATE 2>&1" > "$LOG_DIR/quality-gate-${agent_name}.log"; then
    gum style --foreground 42 "✓ Quality gate passed"
    return 0
  else
    gum style --foreground 196 "✗ Quality gate failed"
    gum style --foreground 240 "See: $LOG_DIR/quality-gate-${agent_name}.log"
    return 1
  fi
}

handle_quality_failure() {
  local agent_name=$1

  echo
  ACTION=$(gum choose \
    "Retry agent with error context" \
    "View error log" \
    "Skip this phase" \
    "Abort pipeline")

  case "$ACTION" in
    *"Retry"*)
      return 2
      ;;
    *"View"*)
      gum pager < "$LOG_DIR/quality-gate-${agent_name}.log"
      handle_quality_failure "$agent_name"
      return $?
      ;;
    *"Skip"*)
      return 0
      ;;
    *"Abort"*)
      exit 1
      ;;
  esac
}

###############################################################################
# MAIN PIPELINE
###############################################################################

main() {
  show_header

  # Show plan overview
  gum style --foreground 99 "Plan Overview:"
  gum style --foreground 240 "Phase 1: Measure baseline (add timing logs)"
  gum style --foreground 240 "Phase 2: Extract functions (refactor for testability)"
  gum style --foreground 240 "Phase 3: Optimize (WalkDir, manual refresh)"
  gum style --foreground 240 "Phase 4: Re-measure (quantify improvements)"
  echo

  if ! gum confirm "Ready to start 4-phase performance improvement?"; then
    gum style --foreground 99 "Cancelled."
    exit 0
  fi

  local index=1
  local total=${#AGENTS[@]}

  for agent_spec in "${AGENTS[@]}"; do
    IFS=':' read -r agent_name task <<< "$agent_spec"

    CURRENT_AGENT="$agent_name"

    show_agent_header "$index" "$total" "$agent_name"

    # SPECIAL HANDLING FOR PHASE 3 (Critical optimization phase)
    if [[ "$agent_name" == "phase3-optimize" ]]; then
      echo
      gum style --foreground 196 --bold "⚠️  CRITICAL PHASE AHEAD"
      gum style --foreground 99 "Phase 3 includes the lazy-loading fix - the most impactful change."
      gum style --foreground 99 "This phase will:"
      gum style --foreground 240 "  - Replace os.ReadDir with filepath.WalkDir"
      gum style --foreground 240 "  - Remove line counting from git diff"
      gum style --foreground 240 "  - Add manual refresh (r/R keys)"
      gum style --foreground 240 "  - Fix collapsed directory scanning bug"
      echo
      gum style --foreground 99 "Before proceeding:"
      gum style --foreground 240 "  1. Ensure git is clean (will auto-commit after success)"
      gum style --foreground 240 "  2. Review PERFORMANCE_PLAN.md lines 491-557"
      gum style --foreground 240 "  3. Be ready to rollback if needed"
      echo
      if ! gum confirm "Ready for Phase 3 optimization?"; then
        gum style --foreground 99 "⏸ Stopped before Phase 3."
        exit 0
      fi
    fi

    local retry=true
    while $retry; do
      if spawn_agent "$agent_name" "$task"; then
        if run_quality_gate "$agent_name"; then
          COMPLETED_AGENTS+=("$agent_name")

          # Auto-commit after each successful phase
          if git rev-parse --git-dir > /dev/null 2>&1; then
            gum style --foreground 99 "Creating git checkpoint..."
            git add -A
            git commit -m "✓ ${agent_name} complete

Orchestrated via gummy-agents
Cost: \$${TOTAL_COST}
Log: ${LOG_DIR}/${agent_name}.log" 2>/dev/null || true
            git tag "${agent_name}-complete" 2>/dev/null || true
            gum style --foreground 42 "✓ Git checkpoint: ${agent_name}-complete"
          fi

          retry=false

          if [ $index -lt $total ]; then
            if ! gum confirm "Proceed to next phase?"; then
              gum style --foreground 99 "⏸ Pipeline paused."
              exit 0
            fi
          fi
        else
          handle_quality_failure "$agent_name"
          case $? in
            0) retry=false ;;
            2)
              # Retry with error context
              error_log=$(cat "$LOG_DIR/quality-gate-${agent_name}.log")
              task="$task

RETRY CONTEXT - Previous attempt failed with:
$error_log

Please fix these errors."
              retry=true
              ;;
            *) exit 1 ;;
          esac
        fi
      else
        retry=false
      fi
    done

    ((index++))
  done

  show_completion

  # Offer to view results
  echo
  if gum confirm "View improvements summary?"; then
    if [ -f "IMPROVEMENTS.txt" ]; then
      gum pager < IMPROVEMENTS.txt
    else
      gum style --foreground 196 "IMPROVEMENTS.txt not found"
    fi
  fi
}

###############################################################################
# ENTRY POINT
###############################################################################

if ! command -v gum &> /dev/null; then
  echo "Error: gum is not installed"
  echo "Install: brew install gum"
  exit 1
fi

if ! command -v claude &> /dev/null; then
  echo "Error: claude CLI is not installed"
  exit 1
fi

if ! command -v jq &> /dev/null; then
  echo "Error: jq is not installed"
  echo "Install: brew install jq"
  exit 1
fi

main
