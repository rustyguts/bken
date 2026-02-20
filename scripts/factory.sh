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

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

handle_sigint() {
  echo ""
  log "Interrupted — shutting down factory."
  exit 0
}

trap handle_sigint INT TERM

log "Factory started (interval: ${POLL_INTERVAL}s). Press Ctrl-C to stop."

while true; do
  log "Dispatching to Claude Code..."

  claude --dangerously-skip-permissions \
    --print \
    --continue \
    "/turn-off-the-lights"

  log "Claude Code session complete. Sleeping ${POLL_INTERVAL}s..."
  sleep "$POLL_INTERVAL"
done
