#!/usr/bin/env bash
# claude-ticketpilot-run.sh — scan for mentions and reply using Claude Code
#
# Usage:
#   ./scripts/claude-ticketpilot-run.sh
#
# Environment variables:
#   TICKETPILOT_BIN  Path to ticketpilot binary (default: bin/ticketpilot)
#   ENV_FILE         Path to .env file (default: .env in project root)
#   AGENTS_FILE      Path to agents.md (default: agents.md in project root)
#
# Cron example (every 5 minutes):
#   */5 * * * * /path/to/scripts/claude-ticketpilot-run.sh >> /var/log/ticketpilot.log 2>&1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

ENV_FILE="${ENV_FILE:-$PROJECT_DIR/.env}"
TP_ENV_OPT=""
[ -f "$ENV_FILE" ] && TP_ENV_OPT="--env-file $ENV_FILE"
AGENTS_FILE="${AGENTS_FILE:-$PROJECT_DIR/agents.md}"

if [ -n "${TICKETPILOT_BIN:-}" ]; then
  TP="$TICKETPILOT_BIN"
elif [ -x "$PROJECT_DIR/bin/ticketpilot" ]; then
  TP="$PROJECT_DIR/bin/ticketpilot"
elif command -v ticketpilot &>/dev/null; then
  TP="ticketpilot"
else
  echo "ERROR: ticketpilot not found. Run: make build" >&2
  exit 1
fi

log() { echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"; }

# ---------------------------------------------------------------------------
# Auth check
# ---------------------------------------------------------------------------

if ! command -v claude &>/dev/null; then
  echo "ERROR: 'claude' not found in PATH. Install Claude Code: https://claude.ai/download" >&2
  exit 1
fi
if ! claude auth status &>/dev/null; then
  echo "ERROR: Not logged in to Claude Code. Run: claude auth login" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Scan
# ---------------------------------------------------------------------------

log "Scanning for pending mentions..."
SCAN=$($TP scan -v $TP_ENV_OPT)
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
# Call Claude
# ---------------------------------------------------------------------------

log "Calling Claude..."

if [ "$SESSION_ID" = "null" ]; then
  SYSTEM_PROMPT=$(cat "$AGENTS_FILE")
  RESPONSE=$(claude -p "$PROMPT" --system-prompt "$SYSTEM_PROMPT" --output-format json --tools "")
else
  RESPONSE=$(claude --resume "$SESSION_ID" -p "$PROMPT" --output-format json --tools "")
fi

NEW_SESSION_ID=$(echo "$RESPONSE" | jq -r '.session_id')
REPLY_BODY=$(echo "$RESPONSE"     | jq -r '.result')

if [ -z "$REPLY_BODY" ] || [ "$REPLY_BODY" = "null" ]; then
  echo "ERROR: Claude returned empty reply. Aborting without posting." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Post reply
# ---------------------------------------------------------------------------

log "Posting reply (session: $NEW_SESSION_ID)..."
# shellcheck disable=SC2086
$TP reply -v $TP_ENV_OPT \
  --ticket-id "$TICKET_ID" \
  --comment-id "$COMMENT_ID" \
  --session-id "$NEW_SESSION_ID" \
  --body "$REPLY_BODY"

log "Done. Replied to $TYPE '$TITLE' (ticket: $TICKET_ID)"
