# sesssions

_The base of this project is created by **hand** based on [my scripts](https://github.com/mostafaqanbaryan/dotfiles/tree/main/scripts) that now is dropped from the dotfiles repo. But new features are mostly added, again based on my needs and scripts, by AI._

`sesssions` is a CLI tool and interactive fuzzy-finder to manage `tmux` sessions and `git` worktrees, with Docker Compose driven entirely from configuration — **no `docker-compose*.yml` files live in your repositories**.

---

## Features

- **Fuzzy Interactive Session Picker**: Switch between active tmux sessions or launch new sessions from configured project folders.
- **Git Worktree Integration**: Create and destroy isolated git worktrees mapped to tmux sessions with keyboard shortcuts (`Ctrl-A` / `Ctrl-X`) or CLI commands.
- **Compose in config**: The docker-compose definition lives in each project's `docker-compose.dev.yml`, seeded from language templates by `sesssions init`.
- **`sesssions up` / `sesssions down`**: Start / stop a project's stack.
- **Lifecycle automation**: `add-worktree` copies gitignored files, initializes the project, writes a worktree-specific `.envrc` (`CONTAINER_NAME` from the project + branch), then brings the stack up reusing the base project's already-built docker image (no Dockerfile rebuild); `delete-worktree` takes the stack down; when the worktree has uncommitted changes it shows the diff in an fzf dialog so you can discard them and remove it, or keep it.
- **`sesssions init`**: Pick your project's language; `docker-compose.dev.yml` is seeded from the language template (re-init asks before replacing).

---

## Requirements

- [Go](https://go.dev/) (>= 1.22)
- [tmux](https://github.com/tmux/tmux)
- [git](https://git-scm.com/)
- [fzf](https://github.com/junegunn/fzf)
- [docker](https://www.docker.com/) (with compose)

---

## Installation

```bash
go install github.com/mostafaqanbaryan/sesssions/cmd/cli@latest
```

Or build from source:

```bash
git clone https://github.com/mostafaqanbaryan/sesssions.git
cd sesssions
go build -o sesssions ./cmd/cli/main.go
mv sesssions /usr/local/bin/ # Or anywhere in your $PATH
```

---

## Usage

### 1. Interactive Menu (`list`)

Run `sesssions` or `sesssions list` to launch the fuzzy-finder:

```bash
sesssions list
```

#### Keybindings inside fzf:
- **`Enter`**: Attach to selected tmux session or open project directory in a new session.
- **`Ctrl-A`**: Open a branch selector to create a new git worktree + tmux session (brings compose up).
- **`Ctrl-X`**: Teardown and delete the selected worktree session (takes compose down).

---

### 2. CLI Commands

```bash
# Pick your project's language and write .sesssions.yaml (embeds the compose template)
sesssions init

# Start the current project's stack (or a given path)
sesssions up [path]

# Stop the current project's stack (or a given path)
sesssions down [path]

# Create a new git worktree & tmux session from a specific branch
sesssions add-worktree <repo-cwd> <branch-name>

# Teardown and delete a git worktree & tmux session
sesssions delete-worktree <worktree-path>

# Interactively pick a branch to create a worktree for a project
sesssions branches <repo-cwd>

# Show help
sesssions help
```

---

## Configuration

### Global config (`~/.config/sesssions/sesssions.yaml`)

Auto-created on first run. Defines which folders `list` searches and the language templates embedded into new projects:

```yaml
projects:
  - "~"

languages:
  go:
    environment:
      DOMAIN: localhost
    compose: |
      services:
        app:
          build: .
          container_name: ${CONTAINER_NAME}
          # ...
  php:
    environment:
      DOMAIN: localhost
    compose: |
      services:
        php:
          image: php:8.3-fpm
          # ...
```

### Per-project (`docker-compose.dev.yml`)

Created by `sesssions init` in the project root and seeded from the selected language template. This file defines how the project's stack runs and may be edited freely; re-running `sesssions init` asks before replacing it:

```yaml
services:
  app:
    build:
      context: .
      dockerfile: Dockerfile.dev
    container_name: ${CONTAINER_NAME}
    env_file:
      - ./src/.env
    # ...
```

`.sesssions.yaml` in the project root only records the project's language.

`sesssions up` / `sesssions down` (and worktree add/delete) render `docker-compose.dev.yml` to a file under `~/.cache/sesssions/` and run `docker compose ...`. Relative build contexts, env files and bind mounts are rewritten to absolute paths at render time, so they always resolve against the project directory.

### Placeholders

| Placeholder | Meaning |
|---|---|
| `{uid}` | the current user id |
| `{base}` | base name of the project directory |
| `__SESSION_NAME__` | the worktree / tmux session identifier |
| `{go_dlv_port}` / `${go_dlv_port}` | host port for the Go Delve debugger, generated on first run |
| `${CONTAINER_NAME}` | rendered by docker compose from `environment` / shell env |

---

## Notes

- **Compose lives in `docker-compose.dev.yml`**: seeded by `sesssions init` from the language's template, replaced only after explicit confirmation.
- `add-worktree` brings up a new worktree's stack; `delete-worktree` tears it down (both are also available standalone).
- Old `.sesssions.yaml` projects holding an inline `compose:` still work: `up` falls back to it (and then to the global language template) when no `docker-compose.dev.yml` exists.
