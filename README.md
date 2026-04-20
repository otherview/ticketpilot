# TicketPilot

A GitHub Project bot that scans for `@handle` mentions on issues and PRs, invokes an AI agent to craft a reply, and posts it back — maintaining conversation context across turns.

## How it works

1. `ticketpilot scan` — finds the next unprocessed mention in your GitHub Project
2. The shell wrapper calls Claude (or another AI) with the mention context
3. `ticketpilot reply` — posts the AI's response and records the session
4. `ticketpilot create` — creates a new issue and adds it to the project board

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
TICKETPILOT_REPO=owner/repo
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

### `ticketpilot scan`

Finds the next unprocessed `@handle` mention. Returns the mention context as JSON for the AI to process.

```bash
./ticketpilot scan
```

**Output (pending mention):**

```json
{
  "pending": true,
  "ticket_id": "owner/repo#42",
  "comment_id": "comment_abc123",
  "title": "Fix crash on startup",
  "type": "Issue",
  "repo_owner": "owner",
  "repo_name": "repo",
  "issue_number": 42,
  "comment_author": "alice",
  "comment_body": "@YourBotHandle the app crashes when I set X",
  "comment_thread": [
    {"author": "alice", "body": "@YourBotHandle help", "created_at": "..."}
    // ... more thread comments
  ],
  "session_id": "sess-abc"
}
```

**Output (nothing to process):**

```json
{
  "pending": false
}
```

---

### `ticketpilot reply`

Posts the AI-crafted reply back to the GitHub issue. The body comes from `--body` or stdin.

```bash
./ticketpilot reply \
  --ticket-id "owner/repo#42" \
  --comment-id "comment_abc123" \
  --session-id "sess-abc" \
  --body "Hi @alice — this looks like a known issue. Try setting X to Y."
```

**Output:**

```json
{
  "success": true,
  "ticket_id": "owner/repo#42",
  "comment_id": "comment_def456",
  "session_id": "sess-abc"
}
```

The `comment_id` in the response is the ID of the newly posted reply.

---

### `ticketpilot create`

Creates a new GitHub issue and places it on the project board in the specified status column.

```bash
./ticketpilot create \
  --title "Add dark mode support" \
  --body "User requested dark mode. See design spec." \
  --project-column "Todo" \
  --session-id "optional-session-id"
```

**Output:**

```json
{
  "ticket_id": "owner/repo#43",
  "repo_owner": "owner",
  "repo_name": "repo",
  "issue_number": 43,
  "issue_url": "https://github.com/owner/repo/issues/43",
  "project_column": "Todo",
  "session_id": "optional-session-id"
}
```

| Flag | Required | Description |
|------|----------|-------------|
| `--title` | Yes | Issue title |
| `--body` | Yes | Issue body (markdown) |
| `--project-column` | Yes | Status option name (e.g. "Todo", "In Progress", "Done") |
| `--session-id` | No | Session/conversation ID |

---

## Agent behaviour

Customise the bot's persona and instructions in `agents.md`.
