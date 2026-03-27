# ---- Stage 1: Build ticketpilot binary ----
FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY Makefile .
COPY ticketpilot-go/ ticketpilot-go/
RUN make build

# ---- Stage 2: Runtime ----
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git jq \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22 LTS (required for claude CLI)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) \
        signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] \
        https://cli.github.com/packages stable main" \
        | tee /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# Bypass the interactive onboarding wizard (safe to bake in — not sensitive).
# Written to the volume-backed config dir at runtime by the entrypoint.
# (Cannot bake into /data at build time — it's a volume mount point.)

# Go toolchain — copied from builder so the bot can run go commands
COPY --from=builder /usr/local/go /usr/local/go
ENV PATH=$PATH:/usr/local/go/bin

# Non-root user — --dangerously-skip-permissions is blocked for root by Claude Code
RUN useradd -m -s /bin/bash -u 1000 bot

# App
WORKDIR /app
COPY --from=builder --chown=bot:bot /build/bin/ticketpilot bin/ticketpilot
COPY --chown=bot:bot scripts/ scripts/
COPY --chown=bot:bot agents.md .
RUN chmod +x scripts/*.sh

# State and Claude session data live in /data so they survive container rebuilds
RUN mkdir -p /data && chown bot:bot /data
ENV TICKETPILOT_STATE_FILE=/data/state.json
ENV CLAUDE_CONFIG_DIR=/data/.claude

COPY --chown=bot:bot docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

USER bot
ENTRYPOINT ["docker-entrypoint.sh"]
