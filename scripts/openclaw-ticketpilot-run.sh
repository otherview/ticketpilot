#!/usr/bin/env bash
# openclaw-ticketpilot-run.sh — scan for mentions and reply using OpenClaw
#
# Usage:
#   ./scripts/openclaw-ticketpilot-run.sh
#
# Environment variables:
#   TICKETPILOT_BIN  Path to ticketpilot binary (default: bin/ticketpilot)
#   ENV_FILE         Path to .env file (default: .env in project root)
#   AGENTS_FILE      Path to agents.md (default: agents.md in project root)
#
# Cron example (every 5 minutes):
#   */5 * * * * /path/to/scripts/openclaw-ticketpilot-run.sh >> /var/log/ticketpilot.log 2>&1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

ENV_FILE="${ENV_FILE:-$PROJECT_DIR/.env}"
AGENTS_FILE="${AGENTS_FILE:-$PROJECT_DIR/agents.md}"

if [ -n "${TICKETPILOT_BIN:-}" ]; then
  TP="$TICKETPILOT_BIN"
elif [ -x "$PROJECT_DIR/bin/ticketpilot" ]; then
  TP="$PROJECT_DIR/bin/ticketpilot"
else
  echo "ERROR: bin/ticketpilot not found. Run: make build" >&2
  exit 1
fi

log() { echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"; }

# ---------------------------------------------------------------------------
# Auth check
# ---------------------------------------------------------------------------

if ! command -v openclaw &>/dev/null; then
  echo "ERROR: 'openclaw' not found in PATH." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Scan
# ---------------------------------------------------------------------------

log "Scanning for pending mentions..."
SCAN=$($TP scan -v --env-file "$ENV_FILE")
PENDING=$(echo "$SCAN" | jq -r '.pending')

if [ "$PENDING" = "false" ]; then
  log "No pending mentions."
  exit 0
fi

TICKET_ID=$(echo "$SCAN"  | jq -r '.ticket_id')
COMMENT_ID=$(echo "$SCAN" | jq -r '.comment_id')
TITLE=$(echo "$SCAN"      | jq -r '.title')
TYPE=$(echo "$SCAN"       | jq -r '.type')
SESSION_ID=$(echo "$SCAN" | jq -r '.session_id')

log "Pending mention on $TYPE: $TITLE (ticket: $TICKET_ID)"

# ---------------------------------------------------------------------------
# Build prompt
# ---------------------------------------------------------------------------

PROMPT=$(echo "$SCAN" | "$SCRIPT_DIR/prompt.sh")

# ---------------------------------------------------------------------------
# Call OpenClaw
# ---------------------------------------------------------------------------

log "Calling OpenClaw..."

if [ "$SESSION_ID" = "null" ]; then
  SYSTEM_PROMPT=$(cat "$AGENTS_FILE")
  FULL_PROMPT="$SYSTEM_PROMPT

$PROMPT"
  RESPONSE=$(openclaw agent --message "$FULL_PROMPT" --json)
else
  RESPONSE=$(openclaw agent --session-id "$SESSION_ID" --message "$PROMPT" --json)
fi

NEW_SESSION_ID=$(echo "$RESPONSE" | jq -r '.session_id')
REPLY_BODY=$(echo "$RESPONSE"     | jq -r '.content')

if [ -z "$REPLY_BODY" ] || [ "$REPLY_BODY" = "null" ]; then
  echo "ERROR: OpenClaw returned empty reply. Aborting without posting." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Post reply
# ---------------------------------------------------------------------------

log "Posting reply (session: $NEW_SESSION_ID)..."
$TP reply -v \
  --env-file "$ENV_FILE" \
  --ticket-id "$TICKET_ID" \
  --comment-id "$COMMENT_ID" \
  --session-id "$NEW_SESSION_ID" \
  --body "$REPLY_BODY"

log "Done. Replied to $TYPE '$TITLE' (ticket: $TICKET_ID)"
