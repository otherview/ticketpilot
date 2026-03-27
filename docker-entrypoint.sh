#!/usr/bin/env bash
set -euo pipefail

TOKEN_FILE="/data/claude_oauth_token"

# On-boarding: run claude setup-token interactively, save token, then exit.
# Usage: docker compose run --rm -it ticketpilot on-board-claude
if [ "${1:-}" = "on-board-claude" ]; then
  claude setup-token
  echo ""
  read -r -p "Paste the token shown above: " OAUTH_TOKEN
  if [ -z "$OAUTH_TOKEN" ]; then
    echo "ERROR: No token provided." >&2
    exit 1
  fi
  printf '%s' "$OAUTH_TOKEN" > "$TOKEN_FILE"
  chmod 600 "$TOKEN_FILE"
  export CLAUDE_CODE_OAUTH_TOKEN="$OAUTH_TOKEN"
  if claude auth status &>/dev/null; then
    echo "Claude is set up. You can now run: docker compose up -d"
    exit 0
  else
    echo "ERROR: Authentication check failed. Please verify the token and try again." >&2
    rm -f "$TOKEN_FILE"
    exit 1
  fi
fi

# If other arguments are passed, exec them directly — no env setup needed.
if [ "$#" -gt 0 ]; then
  exec "$@"
fi

# Load Claude OAuth token from the volume-persisted file.
if [ ! -f "$TOKEN_FILE" ]; then
  echo "ERROR: Claude is not authenticated." >&2
  echo "Run: docker compose run --rm -it ticketpilot on-board-claude" >&2
  exit 1
fi
export CLAUDE_CODE_OAUTH_TOKEN
CLAUDE_CODE_OAUTH_TOKEN=$(< "$TOKEN_FILE")

# Ensure the onboarding marker exists in the volume-backed config dir.
mkdir -p "${CLAUDE_CONFIG_DIR}"
if [ ! -f "${CLAUDE_CONFIG_DIR}/ONBOARDING_DONE" ]; then
  echo '{"hasCompletedOnboarding":true}' > "${CLAUDE_CONFIG_DIR}/.claude.json"
  touch "${CLAUDE_CONFIG_DIR}/ONBOARDING_DONE"
fi

# Authenticate gh CLI using the GitHub PAT
export GH_TOKEN="${TICKETPILOT_GITHUB_PAT}"

# Configure git to use the PAT transparently for all github.com operations
git config --global url."https://x-access-token:${TICKETPILOT_GITHUB_PAT}@github.com/".insteadOf "https://github.com/"
git config --global user.email "ticketpilot@bot"
git config --global user.name "TicketPilot"

INTERVAL="${TICKETPILOT_INTERVAL:-60}"

echo "TicketPilot starting (interval: ${INTERVAL}s)"

while true; do
    /app/scripts/claude-ticketpilot-run.sh || true
    echo "sleeping ${INTERVAL}s..."
    sleep "$INTERVAL"
done
