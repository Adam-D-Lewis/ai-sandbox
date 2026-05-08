FROM debian:bookworm-slim

ARG PI_VERSION=latest
ARG MISE_VERSION=v2026.5.3
ARG OMZ_SHA=e64912e0c1eaa32181c3b5e5e4bf8042ecd0e8a7
ARG TARGETARCH
# Match host uid/gid so bind-mounted files stay writable. build.sh passes the
# real values from `id -u` / `id -g` / $HOME; the defaults below are only used
# when invoking `docker build` directly without those args.
ARG AGENT_UID=1000
ARG AGENT_GID=1000
ARG AGENT_HOME=/home/agent

ENV DEBIAN_FRONTEND=noninteractive

# mise (jdx/mise) lives system-wide so root-built tool installs are visible at
# runtime under the unprivileged agent user. Shims dir first in PATH means
# `node`, `go`, `python`, `uv`, `pixi`, `claude` resolve to whatever is pinned
# in /etc/mise/config.toml — no per-user mise activation needed.
ENV MISE_DATA_DIR=/usr/local/share/mise
ENV MISE_CONFIG_DIR=/etc/mise
ENV MISE_CACHE_DIR=/var/cache/mise
ENV PATH=/usr/local/share/mise/shims:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Base tools. gnupg dropped — was only used for the (now gone) NodeSource keyring;
# zero callers remain. mise will fall back to checksum-only verification for
# tarballs whose signing it can't check, which is fine for our threat model.
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends \
      ca-certificates curl git tar sudo \
      tini zsh \
 && rm -rf /var/lib/apt/lists/* /var/cache/apt/* /var/log/apt/*

# mise — official installer (https://mise.run). MISE_VERSION pins the release;
# MISE_INSTALL_PATH puts the binary system-wide.
RUN set -e \
 && curl -fsSL https://mise.run \
      | env MISE_VERSION=${MISE_VERSION} MISE_INSTALL_PATH=/usr/local/bin/mise sh \
 && mise --version

# Tool versions live in /etc/mise/config.toml. One source of truth — bump here,
# rebuild image. `mise install` reads it, downloads tools to MISE_DATA_DIR.
RUN set -e \
 && mkdir -p "$MISE_CONFIG_DIR" "$MISE_CACHE_DIR" "$MISE_DATA_DIR" \
 && cat > "$MISE_CONFIG_DIR/config.toml" <<'TOML'
[tools]
node   = "24"
go     = "1.26.2"
python = "3.14.4"
uv     = "0.11.11"
pixi   = "0.68.0"
gh     = "2.92.0"
TOML

RUN set -e \
 && mise install \
 && mise reshim \
 && rm -rf "$MISE_CACHE_DIR"/* /tmp/* \
 && rm -rf "$MISE_DATA_DIR"/installs/go/*/test \
           "$MISE_DATA_DIR"/installs/go/*/src/cmd \
 && find "$MISE_DATA_DIR"/installs/node/*/lib/node_modules/npm \
        \( -name man -o -name docs -o -name changelogs \) \
        -type d -prune -exec rm -rf {} + \
 && node --version \
 && go version \
 && python --version \
 && uv --version \
 && pixi --version \
 && gh --version

# claude-code via mise's node, then reshim so /usr/local/share/mise/shims/claude
# appears for both root build and the agent user at runtime.
RUN set -e \
 && npm install -g --no-audit --no-fund @anthropic-ai/claude-code \
 && npm cache clean --force \
 && rm -rf /root/.npm \
 && mise reshim \
 && claude --version

# Install pi from upstream release tarball. Tarball ships docs/, examples/,
# CHANGELOG.md, README.md, assets/, export-html/ — none touched by the running
# binary, all stripped post-extract. The runtime needs `pi` + `package.json` +
# `theme/` + `photon_rs_bg.wasm`.
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
 && rm -rf /opt/pi/docs /opt/pi/examples /opt/pi/assets /opt/pi/export-html \
 && ln -sf /opt/pi/pi /usr/local/bin/pi \
 && rm /tmp/pi.tgz \
 && /usr/local/bin/pi --version

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

# Confirm shims resolve under the unprivileged user too — catches perm bugs at
# build time rather than first `psb` shell.
RUN set -e \
 && su -s /bin/sh agent -c ' \
      mise   --version && \
      node   --version && \
      go     version   && \
      python --version && \
      uv     --version && \
      pixi   --version && \
      gh     --version && \
      claude --version && \
      pi     --version    \
    '

USER agent
WORKDIR ${AGENT_HOME}

# oh-my-zsh — clone at pinned commit, write minimal zshrc with agent prompt.
RUN set -e \
 && git clone https://github.com/ohmyzsh/ohmyzsh.git "$HOME/.oh-my-zsh" \
 && git -C "$HOME/.oh-my-zsh" -c advice.detachedHead=false checkout "$OMZ_SHA" \
 && rm -rf "$HOME/.oh-my-zsh/.git" \
 && find "$HOME/.oh-my-zsh/plugins" -mindepth 1 -maxdepth 1 -type d \
        ! -name git -exec rm -rf {} + \
 && find "$HOME/.oh-my-zsh/themes" -mindepth 1 -maxdepth 1 -type f \
        ! -name 'robbyrussell.zsh-theme' -exec rm -f {} + \
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
