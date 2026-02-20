#!/usr/bin/env bash
# factory.sh — Run Claude Code in a loop every 30 seconds.
#
# Usage:
#   ./scripts/factory.sh              # default 30-second interval
#   POLL_INTERVAL=60 ./scripts/factory.sh
#
# Stop with Ctrl-C or SIGTERM.

set -euo pipefail

POLL_INTERVAL="${POLL_INTERVAL:-30}"

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_DIR"

CHILD_PID=""
SHUTDOWN=false

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

handle_sigint() {
  echo ""
  log "Interrupted — shutting down factory."
  SHUTDOWN=true
  if [[ -n "$CHILD_PID" ]]; then
    # Kill the entire process group to catch both claude and jq
    kill -- -"$CHILD_PID" 2>/dev/null || kill "$CHILD_PID" 2>/dev/null || true
  fi
  exit 0
}

trap handle_sigint INT TERM

log "Factory started (interval: ${POLL_INTERVAL}s). Press Ctrl-C to stop."

while true; do
  log "Dispatching to Claude Code..."

  # Run in a subshell so claude+jq share a process group we can kill together
  (
    claude --dangerously-skip-permissions \
      --print \
      --continue \
      --output-format stream-json \
      --verbose \
      --include-partial-messages \
      "/turn-off-the-lights" 2>&1 | \
      jq -rj 'select(.type == "stream_event" and .event.delta.type? == "text_delta") | .event.delta.text'
  ) &
  CHILD_PID=$!
  wait "$CHILD_PID" || true
  CHILD_PID=""
  echo "" # newline after streamed output

  $SHUTDOWN && break

  log "Claude Code session complete. Sleeping ${POLL_INTERVAL}s..."

  sleep "$POLL_INTERVAL" &
  CHILD_PID=$!
  wait "$CHILD_PID" || true
  CHILD_PID=""

  $SHUTDOWN && break
done