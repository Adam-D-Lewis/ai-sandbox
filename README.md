# ai-sandbox

[![ci](https://github.com/aktech/ai-sandbox/actions/workflows/ci.yml/badge.svg)](https://github.com/aktech/ai-sandbox/actions/workflows/ci.yml)
[![image](https://github.com/aktech/ai-sandbox/actions/workflows/image.yml/badge.svg)](https://github.com/aktech/ai-sandbox/actions/workflows/image.yml)
[![release](https://github.com/aktech/ai-sandbox/actions/workflows/release.yml/badge.svg)](https://github.com/aktech/ai-sandbox/actions/workflows/release.yml)

Run AI coding agents (Claude Code, pi, etc.) in a Docker container instead
of directly on your laptop. Each project gets its own container, and the
container can only see the folders you explicitly mount in. If the agent
goes wrong, you lose the container — not your home directory.

The CLI is called `psb`. One command spins up (or re-enters) a sandbox
for the current project and drops you into a shell where `claude` and
`pi` are already installed.

## Install

```sh
make build       # builds ./bin/psb — put it on your $PATH
psb build        # builds the Docker image (one-time, ~5 min)
```

You need Docker running. On macOS, [colima](https://github.com/abiosoft/colima)
works well (`colima start`).

## Use it

```sh
cd ~/dev/my-project
psb              # first run: creates a container, drops you into zsh
                 # next runs: re-enters the same container
```

Inside the container, run `claude` or `pi` like you normally would.

| Command          | What it does                                  |
|------------------|-----------------------------------------------|
| `psb`            | Enter the sandbox for the current project.    |
| `psb stop`       | Stop the current project's container.         |
| `psb rm`         | Delete the current project's container.       |
| `psb ls`         | List all sandboxes.                           |
| `psb status`     | Show one container's status.                  |
| `psb build`      | Rebuild the image.                            |

## What gets mounted

By default the container only sees the project directory. Anything else
you want available — your `.gitconfig`, dotfiles, a shared library — you
list in a config file:

`~/.config/ai-sandbox/config.json`

```json
{
  "default": {
    "mounts": [
      "{{HOME}}/.gitconfig",
      "{{HOME}}/.claude/settings.json",
      "{{CWD}}"
    ]
  },
  "projects": {
    "/Users/me/dev/my-project": {
      "extra_mounts": ["~/dev/shared-lib"],
      "memory": "8g",
      "cpus": 4
    }
  }
}
```

- `mounts` — the full list. Each entry becomes a `-v src:src` bind mount.
- `extra_mounts` — appended to `mounts`. Use for per-project additions.
- `memory`, `cpus`, `image` — optional per-project overrides.

Placeholders: `{{HOME}}`, `{{CWD}}`, `{{SHARED_DIR}}`. Shell-style `~/`
and `$VAR` also work. Paths that don't exist on the host are skipped
with a warning.

## Environment overrides

| Var                | Default                              |
|--------------------|--------------------------------------|
| `PSB_IMAGE_NAME`   | `ai-sandbox-pi:latest`               |
| `PSB_MEMORY`       | `4g`                                 |
| `PSB_CPUS`         | `2`                                  |
| `PSB_SHARED_DIR`   | `~/sb-shared`                        |
| `PSB_CONFIG_FILE`  | `~/.config/ai-sandbox/config.json`   |
| `ANTHROPIC_API_KEY`| passed through to the container      |

## What's in the image

Debian slim with `claude`, `pi`, Node, Go, `uv`, `pixi`, `git`, `zsh`.
The container runs as a non-root user whose UID/GID matches your host,
so files you create inside stay writable from outside.
