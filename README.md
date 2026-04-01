# TicketPilot

A GitHub Project bot that scans for `@handle` mentions on issues and PRs, invokes an AI agent to craft a reply, and posts it back — maintaining conversation context across turns.

## How it works

1. `ticketpilot scan` — finds the next unprocessed mention in your GitHub Project
2. The shell wrapper calls Claude (or another AI) with the mention context
3. `ticketpilot reply` — posts the AI's response and records the session

## Requirements

- Go 1.22+
- [Claude Code](https://claude.ai/download) (`claude` CLI) authenticated
- GitHub PAT with `repo` + `project` scopes

## Setup

```env
# .env
TICKETPILOT_GITHUB_PAT=ghp_...
TICKETPILOT_GITHUB_HANDLE=@YourBotHandle
TICKETPILOT_PROJECT_URL=https://github.com/users/you/projects/1
```

```bash
make build
```

## Usage

```bash
# One-shot run (scan → AI → reply)
./scripts/claude-ticketpilot-run.sh

# Or as a cron job (every 5 minutes)
*/5 * * * * /path/to/scripts/claude-ticketpilot-run.sh >> /var/log/ticketpilot.log 2>&1
```

## CLI reference

```
ticketpilot scan              # prints JSON: pending mention or {pending:false}
ticketpilot reply             # posts reply; --ticket-id --comment-id --session-id required
ticketpilot create            # creates issue + adds to configured project; --repo-owner --repo-name --title required
```

## Agent behaviour

Customise the bot's persona and instructions in `agents.md`.
