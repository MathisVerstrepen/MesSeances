# syntax=docker/dockerfile:1
FROM golang:1.26.0-bookworm AS go
FROM node:22.23.1-bookworm-slim
ARG REPO_URL
ARG REPO_REF
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash build-essential ca-certificates curl git openssh-server postgresql-client \
    procps python3 ripgrep rsync tmux unzip zsh chromium \
    && rm -rf /var/lib/apt/lists/* /etc/ssh/ssh_host_*
COPY --from=go /usr/local/go /usr/local/go
COPY bin/ /usr/local/bin/
ENV PATH="/home/mathis/.local/bin:/home/mathis/.bun/bin:/home/mathis/.opencode/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
    DATABASE_URL="postgres://movieflow:movieflow@postgres:5432/movieflow?sslmode=disable" \
    TEST_DATABASE_URL="postgres://movieflow:movieflow@postgres:5432/movieflow?sslmode=disable" \
    NUXT_API_BASE="http://localhost:8080" NUXT_PUBLIC_API_BASE="" \
    CHROME_BIN="chromium" HOST="0.0.0.0"
RUN npm install -g npm@10.9.8 && useradd --uid 1001 --create-home --shell /bin/bash mathis \
    && passwd -d mathis && mkdir -p /run/sshd /home/mathis/projects /home/mathis/.local/bin \
    && ln -s /usr/local/bin/codebase-memory-mcp /home/mathis/.local/bin/codebase-memory-mcp \
    && printf '%s\n' 'PasswordAuthentication no' 'KbdInteractiveAuthentication no' \
       'PermitRootLogin no' 'AllowUsers mathis' 'UsePAM yes' > /etc/ssh/sshd_config.d/orca.conf \
    && printf '%s\n' 'export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"' \
       '[ ! -f /etc/orca-workspace.env ] || . /etc/orca-workspace.env' > /etc/profile.d/orca.sh \
    && printf '%s\n' '[ ! -f /etc/orca-workspace.env ] || . /etc/orca-workspace.env' >> /etc/zsh/zshenv \
    && chown -R mathis:mathis /home/mathis
COPY --chown=mathis:mathis opencode/ /home/mathis/.config/opencode/
USER mathis
WORKDIR /home/mathis
RUN npm --prefix /home/mathis/.config/opencode ci \
    && opencode --version && bun --version && rtk --version && go version && node --version
RUN --mount=type=secret,id=github_token,uid=1001 \
    set -eu; export GH_TOKEN="$(cat /run/secrets/github_token)" GIT_TERMINAL_PROMPT=0; \
    askpass="$(mktemp)"; trap 'rm -f "$askpass"' EXIT; \
    printf '%s\n' '#!/bin/sh' 'case "$1" in *Username*) printf "%s\n" x-access-token;; *) printf "%s\n" "$GH_TOKEN";; esac' > "$askpass"; \
    chmod 700 "$askpass"; export GIT_ASKPASS="$askpass"; \
    git clone --branch "$REPO_REF" "$REPO_URL" /home/mathis/projects/messeances; \
    unset GH_TOKEN GIT_ASKPASS; \
    cd /home/mathis/projects/messeances; go -C api mod download; npm --prefix web ci
USER root
WORKDIR /home/mathis/projects/messeances
EXPOSE 22 3000 8080
ENTRYPOINT ["/usr/local/bin/orca-docker-ssh-entrypoint"]
