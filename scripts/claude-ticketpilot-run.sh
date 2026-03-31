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
_scan_out=$(mktemp)
# shellcheck disable=SC2086
$TP scan -v $TP_ENV_OPT > "$_scan_out"   # debug logs → stderr (visible inline); JSON → file
SCAN=$(cat "$_scan_out")
rm -f "$_scan_out"
PENDING=$(echo "$SCAN" | jq -r '.pending')

if [ "$PENDING" = "false" ]; then
  log "No pending mentions."
  exit 0
fi

TICKET_ID=$(echo "$SCAN"    | jq -r '.ticket_id')
COMMENT_ID=$(echo "$SCAN"   | jq -r '.comment_id')
TITLE=$(echo "$SCAN"        | jq -r '.title')
TYPE=$(echo "$SCAN"         | jq -r '.type')
SESSION_ID=$(echo "$SCAN"   | jq -r '.session_id')
REPO_OWNER=$(echo "$SCAN"   | jq -r '.repo_owner')
REPO_NAME=$(echo "$SCAN"    | jq -r '.repo_name')
ISSUE_NUMBER=$(echo "$SCAN" | jq -r '.issue_number')

THREAD_LEN=$(echo "$SCAN" | jq -r '.thread | length')
log "Pending mention on $TYPE: $TITLE (ticket: $TICKET_ID, thread: ${THREAD_LEN} comments, session: ${SESSION_ID})"

# ---------------------------------------------------------------------------
# Prepare repository
# ---------------------------------------------------------------------------

export REPO_DIR REPO_OWNER REPO_NAME ISSUE_NUMBER
export BOT_HANDLE="${TICKETPILOT_GITHUB_HANDLE:-}"

if [ -n "$REPO_OWNER" ] && [ "$REPO_OWNER" != "null" ] && \
   [ -n "$REPO_NAME"  ] && [ "$REPO_NAME"  != "null" ]; then
  REPO_DIR="/data/repos/${REPO_OWNER}/${REPO_NAME}"
  if [ -d "$REPO_DIR/.git" ]; then
    log "Fetching latest refs for ${REPO_OWNER}/${REPO_NAME}..."
    git -C "$REPO_DIR" fetch --all --quiet
  else
    log "Cloning ${REPO_OWNER}/${REPO_NAME}..."
    mkdir -p "$(dirname "$REPO_DIR")"
    git clone "https://github.com/${REPO_OWNER}/${REPO_NAME}" "$REPO_DIR" --quiet
  fi
else
  REPO_DIR=""
  log "WARNING: repo info missing from scan output, skipping clone."
fi

# ---------------------------------------------------------------------------
# Build prompt
# ---------------------------------------------------------------------------

PROMPT=$(echo "$SCAN" | "$SCRIPT_DIR/prompt.sh")

# ---------------------------------------------------------------------------
# Call Claude
# ---------------------------------------------------------------------------

SYSTEM_PROMPT=$(cat "$AGENTS_FILE")
if [ "$SESSION_ID" = "null" ]; then
  log "Calling Claude (new session)..."
  RESPONSE=$(claude -p "$PROMPT" \
    --system-prompt "$SYSTEM_PROMPT" \
    --output-format json \
    --dangerously-skip-permissions)
else
  log "Calling Claude (resuming session: $SESSION_ID)..."
  # Attempt to resume; fall back to a fresh conversation if the session is gone.
  RESPONSE=$(claude --resume "$SESSION_ID" -p "$PROMPT" \
    --output-format json \
    --dangerously-skip-permissions 2>/dev/null) || {
    log "Session $SESSION_ID not found — starting fresh conversation."
    RESPONSE=$(claude -p "$PROMPT" \
      --system-prompt "$SYSTEM_PROMPT" \
      --output-format json \
      --dangerously-skip-permissions)
  }
fi

NEW_SESSION_ID=$(echo "$RESPONSE" | jq -r '.session_id')
REPLY_BODY=$(echo "$RESPONSE"     | jq -r '.result')
REPLY_LEN=${#REPLY_BODY}
log "Claude responded (session: $NEW_SESSION_ID, reply: ${REPLY_LEN} chars)"

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
