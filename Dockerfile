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

# Bypass the interactive onboarding wizard (safe to bake in — not sensitive)
RUN echo '{"hasCompletedOnboarding":true}' > /root/.claude.json

# Go toolchain — copied from builder so the bot can run go commands
COPY --from=builder /usr/local/go /usr/local/go
ENV PATH=$PATH:/usr/local/go/bin

# App
WORKDIR /app
COPY --from=builder /build/bin/ticketpilot bin/ticketpilot
COPY scripts/ scripts/
COPY agents.md .
RUN chmod +x scripts/*.sh

# State lives in a dedicated directory so it can be mounted as a named volume
RUN mkdir -p /data
ENV TICKETPILOT_STATE_FILE=/data/state.json

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENTRYPOINT ["docker-entrypoint.sh"]
