#!/usr/bin/env bash
# Reads scan JSON from stdin, writes prompt text to stdout.
#
# Usage:
#   echo "$SCAN" | ./scripts/prompt.sh
#   ./bin/ticketpilot scan --env-file .env | ./scripts/prompt.sh

set -euo pipefail

SCAN=$(cat)

TITLE=$(echo "$SCAN"         | jq -r '.title')
TYPE=$(echo "$SCAN"          | jq -r '.type')
COMMENT_AUTHOR=$(echo "$SCAN"| jq -r '.comment_author')
COMMENT_BODY=$(echo "$SCAN"  | jq -r '.comment_body')
SESSION_ID=$(echo "$SCAN"    | jq -r '.session_id')

THREAD=$(echo "$SCAN" | jq -r '
  (.comment_thread // [])
  | map("[\(.author)]: \(.body)")
  | join("\n---\n")
')

if [ "$SESSION_ID" = "null" ]; then
  SESSION_CONTEXT="This is a new conversation — the thread below is the full history."
else
  SESSION_CONTEXT="This is a continuation — the thread below contains only new messages since the last reply."
fi

# Repo context — injected by claude-ticketpilot-run.sh via env vars
REPO_LINE=""
if [ -n "${REPO_DIR:-}" ] && [ "${REPO_DIR:-}" != "null" ]; then
  BRANCH_PREFIX="${BOT_HANDLE:-bot}/${ISSUE_NUMBER:-0}"
  REPO_LINE="Repository: ${REPO_OWNER}/${REPO_NAME}
Local path: ${REPO_DIR}
Branch prefix: ${BOT_HANDLE}/<issue-number>-<short-description>  (e.g. ${BRANCH_PREFIX}-short-description)"
fi

cat <<EOF
$SESSION_CONTEXT

Ticket: $TITLE ($TYPE)
${REPO_LINE:+
$REPO_LINE
}
Thread:
$THREAD

Mention by @$COMMENT_AUTHOR:
$COMMENT_BODY

Address the mention. Use the available tools as needed — read code, run commands, create branches, open PRs. Report what you did or ask a specific question if you need more information.
EOF
