#!/usr/bin/env bash
# Build the ai-sandbox/pi image for the host architecture.
set -euo pipefail

cd "$(dirname "$0")"

IMAGE="${PSB_IMAGE_NAME:-ai-sandbox-pi:latest}"
PI_VERSION="${PI_VERSION:-latest}"

# Bake host's uid/gid/$HOME into the image so bind-mounted files resolve at the
# same path inside the container and stay writable by the host user.
AGENT_UID="$(id -u)"
AGENT_GID="$(id -g)"
AGENT_HOME="${HOME:?HOME not set}"

echo "→ building $IMAGE (pi=$PI_VERSION, home=$AGENT_HOME, uid=$AGENT_UID, gid=$AGENT_GID)"
docker build \
  --build-arg "PI_VERSION=$PI_VERSION" \
  --build-arg "AGENT_UID=$AGENT_UID" \
  --build-arg "AGENT_GID=$AGENT_GID" \
  --build-arg "AGENT_HOME=$AGENT_HOME" \
  -t "$IMAGE" \
  .

echo "✓ built $IMAGE"
docker images --format 'table {{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}' "$IMAGE"
