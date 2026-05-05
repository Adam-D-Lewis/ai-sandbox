FROM debian:bookworm-slim

ARG PI_VERSION=latest
ARG NODE_MAJOR=24
ARG GO_VERSION=1.26.2
ARG OMZ_SHA=e64912e0c1eaa32181c3b5e5e4bf8042ecd0e8a7
ARG TARGETARCH
# Match host uid/gid so bind-mounted files stay writable. build.sh passes the
# real values from `id -u` / `id -g` / $HOME; the defaults below are only used
# when invoking `docker build` directly without those args.
ARG AGENT_UID=1000
ARG AGENT_GID=1000
ARG AGENT_HOME=/home/agent

ENV DEBIAN_FRONTEND=noninteractive

# Base tools (no nodejs from debian — replaced by NodeSource below for Node 24).
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends \
      ca-certificates curl git tar gnupg sudo \
      tini zsh \
 && rm -rf /var/lib/apt/lists/*

# NodeSource Node.js (current major), then claude-code globally.
RUN set -e \
 && curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - \
 && apt-get install -y -qq --no-install-recommends nodejs \
 && rm -rf /var/lib/apt/lists/* \
 && node --version \
 && npm --version \
 && npm install -g --no-audit --no-fund @anthropic-ai/claude-code \
 && claude --version

# uv (Python package manager) + pixi (cross-language env manager).
# Both install as single binaries; place under /usr/local/bin so all users see them.
RUN set -e \
 && curl -LsSf https://astral.sh/uv/install.sh \
      | env UV_UNMANAGED_INSTALL=/usr/local/bin INSTALLER_NO_MODIFY_PATH=1 sh \
 && uv --version \
 && curl -fsSL https://pixi.sh/install.sh \
      | env PIXI_HOME=/usr/local PIXI_NO_PATH_UPDATE=1 bash \
 && pixi --version

# Install pi from upstream release tarball.
# Ships as a single bun-compiled binary plus sibling files (package.json, theme/,
# photon_rs_bg.wasm). Extract whole tree, then symlink the entry binary.
RUN set -e \
 && case "$TARGETARCH" in \
      arm64) ARCH=arm64 ;; \
      amd64) ARCH=x64 ;; \
      *) echo "unsupported arch: $TARGETARCH"; exit 1 ;; \
    esac \
 && URL="https://github.com/badlogic/pi-mono/releases/${PI_VERSION}/download/pi-linux-${ARCH}.tar.gz" \
 && [ "$PI_VERSION" = "latest" ] && URL="https://github.com/badlogic/pi-mono/releases/latest/download/pi-linux-${ARCH}.tar.gz" || true \
 && curl -fsSL -o /tmp/pi.tgz "$URL" \
 && mkdir -p /opt \
 && tar -xzf /tmp/pi.tgz -C /opt \
 && ln -sf /opt/pi/pi /usr/local/bin/pi \
 && rm /tmp/pi.tgz \
 && /usr/local/bin/pi --version

# Go toolchain — official binary tarball pinned to GO_VERSION.
RUN set -e \
 && case "$TARGETARCH" in \
      arm64) ARCH=arm64 ;; \
      amd64) ARCH=amd64 ;; \
      *) echo "unsupported arch: $TARGETARCH"; exit 1 ;; \
    esac \
 && curl -fsSL -o /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" \
 && tar -C /usr/local -xzf /tmp/go.tgz \
 && rm /tmp/go.tgz \
 && /usr/local/go/bin/go version
ENV PATH=/usr/local/go/bin:$PATH

# Create non-root agent user with HOME matching host's so bind-mounted ~/.pi/agent
# resolves at the same path inside the container. -o allows duplicate uid/gid
# (gid 20 conflicts with debian's `dialout`).
#
# Pre-create the agent state dirs (~/.pi/agent, ~/.claude, ~/dev, ~/sb-shared) and
# chown to agent. Without this, docker auto-creates them as root when individual
# files inside are bind-mounted, leaving the parent dir un-writable for agent
# (pi tries to mkdir bin/, write settings.json.lock, etc. — all fail with EACCES).
RUN set -e \
 && groupadd -o -g "$AGENT_GID" hoststaff \
 && useradd  -o -u "$AGENT_UID" -g "$AGENT_GID" -d "$AGENT_HOME" -M -s /bin/zsh agent \
 && mkdir -p \
      "$AGENT_HOME/.pi/agent" \
      "$AGENT_HOME/.pi/agent/bin" \
      "$AGENT_HOME/.claude" \
      "$AGENT_HOME/dev" \
      "$AGENT_HOME/sb-shared" \
 && chown -R agent:hoststaff "$AGENT_HOME" \
 && echo 'agent ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/agent \
 && chmod 0440 /etc/sudoers.d/agent

USER agent
WORKDIR ${AGENT_HOME}

# oh-my-zsh — clone at pinned commit, write minimal zshrc with agent prompt.
RUN set -e \
 && git clone https://github.com/ohmyzsh/ohmyzsh.git "$HOME/.oh-my-zsh" \
 && git -C "$HOME/.oh-my-zsh" -c advice.detachedHead=false checkout "$OMZ_SHA" \
 && cat > "$HOME/.zshrc" <<'ZSHRC'
export ZSH="$HOME/.oh-my-zsh"
ZSH_THEME="robbyrussell"
plugins=(git)
source $ZSH/oh-my-zsh.sh
PROMPT='%F{cyan}  agent%f %F{magenta}%m%f %F{blue}%~%f %F{green}❯%f '
ZSHRC

# tini = small init that reaps zombies + handles signals cleanly. Keeps container
# alive for `docker exec` sessions without needing `sleep infinity`.
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["sleep", "infinity"]
