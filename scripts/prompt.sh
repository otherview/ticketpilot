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
  .comment_thread
  | map("[\(.author)]: \(.body)")
  | join("\n---\n")
')

if [ "$SESSION_ID" = "null" ]; then
  SESSION_CONTEXT="This is a new conversation — the thread below is the full history."
else
  SESSION_CONTEXT="This is a continuation — the thread below contains only new messages since the last reply."
fi

cat <<EOF
$SESSION_CONTEXT

Ticket: $TITLE ($TYPE)

Thread:
$THREAD

Mention by @$COMMENT_AUTHOR:
$COMMENT_BODY

Write a helpful, concise reply addressing the mention above. Be direct. No pleasantries. Output only the reply text, nothing else.
EOF
