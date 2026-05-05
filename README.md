# ai-sandbox / psb

Per-project Docker sandbox for running coding agents (`pi`, `claude`)
locally on macOS via colima. One container per project directory, with
your `~/.pi/agent`, `~/.claude`, dotfiles, and gitconfig bind-mounted in.

## Quick start

```sh
colima start             # one-time: bring up the docker VM
make build               # builds bin/psb
psb build                # builds the ai-sandbox-pi:latest image
cd ~/dev/some-project
psb                      # creates psb-some-project, drops into zsh
```

Inside the container run `pi` or `claude` as you normally would.

## Commands

| Command           | What it does                                     |
|-------------------|--------------------------------------------------|
| `psb`             | Create or attach + shell into `psb-<project>`.   |
| `psb stop`        | Stop the project's container.                    |
| `psb rm [n...]`   | Destroy current container, or named ones.        |
| `psb status`      | Show one container's status.                     |
| `psb ls`          | List all `psb-*` containers (uptime, cpu, mem).  |
| `psb build`       | (Re)build `ai-sandbox-pi:latest`.                |

## Config

`~/.config/ai-sandbox/config.json` (JSON):

```json
{
  "default": {
    "mounts": [
      "{{HOME}}/.gitconfig",
      "{{HOME}}/.claude/settings.json",
      "{{HOME}}/dev/dotfiles",
      "{{SHARED_DIR}}",
      "{{CWD}}"
    ]
  },
  "projects": {
    "/Users/me/dev/some-project": {
      "extra_mounts": ["~/dev/shared-lib"],
      "memory": "8g",
      "cpus": 4
    }
  }
}
```

Keys:
- `mounts` — full bind-mount list (one entry per `-v src:src`).
- `extra_mounts` — appended after `mounts`. Useful for per-project additions.
- `memory`, `cpus` — resource limits passed to docker.
- `image` — override image tag.

Placeholders inside mount strings: `{{HOME}}`, `{{CWD}}`, `{{SHARED_DIR}}`,
plus shell `~/` and `$VAR` expansion. Missing paths are silently skipped
with a warning.

## Env overrides

| Var                | Default                                |
|--------------------|----------------------------------------|
| `PSB_IMAGE_NAME`   | `ai-sandbox-pi:latest`                 |
| `PSB_MEMORY`       | `4g`                                   |
| `PSB_CPUS`         | `2`                                    |
| `PSB_SHARED_DIR`   | `~/sb-shared`                          |
| `PSB_CONFIG_FILE`  | `~/.config/ai-sandbox/config.json`     |
| `HOMELAB_URL`      | passed through into the container      |
| `ANTHROPIC_API_KEY`| passed through (claude API auth)       |

## Image contents

Debian bookworm-slim plus: zsh + oh-my-zsh, Node 24, `pi`, `claude`,
Go 1.26, `uv`, `pixi`, `git`, `tini`. Agent user UID/GID matches macOS
host (501/20) so bind-mounted files stay writable.

## Layout

```
main.go                          CLI: dispatch, lifecycle, ls/rm/status
internal/mountresolver/          mount config → final -v src:src list
Dockerfile, build.sh             image build
Makefile                         `make build` → bin/psb
```
