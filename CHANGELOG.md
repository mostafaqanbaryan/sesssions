# Changelog

All notable changes to this project are documented in this file.

## [0.1.0] - 2026-09-17

### Added
- `sesssions version` command (and `--version` / `-v`).
- Per-project `docker-compose.dev.yml`: `sesssions init` seeds it from the chosen language template (with `{uid}` baked to the current user), asks for confirmation before replacing an existing file, and `up`/`down` use it as the authoritative compose source.
- Global config template versioning: `sesssions init` refreshes the bundled `go`/`php` templates in place while preserving the `projects` list and custom languages.
- `go_dlv_port`: generated with a random free port when unset, printed, and persisted to the project's `.envrc`; the Go template maps it to the container's Delve port (2345).
- Relative build contexts, `env_file` paths and bind mounts are rewritten to absolute paths at render time, so they always resolve against the project directory.
- Go template builds via `Dockerfile.dev` and uses project-relative cache volumes (`./.go-mod-cache`, `./.go-build-cache`).
- Worktree creation copies the files listed in the base repo's `.gitignore` into the new worktree (shown as a checklist), runs project init to seed `.sesssions.yaml` + `docker-compose.dev.yml`, sets `.envrc` so the worktree gets its own `CONTAINER_NAME` (project__branch) and `go_dlv_port`, then starts the compose stack reusing the base project's already-built docker images (`<project>_<service>`) instead of rebuilding the Dockerfile.
- Worktree deletion takes the compose stack down first; when the worktree has uncommitted changes it shows the diff in an fzf dialog and lets the user either discard the changes and force-remove the worktree or keep it.
- `sesssions branches <repo>` resolves a relative repo path, so picking a branch no longer creates a broken `.__<branch>` session / mislocated worktree ("can't find pane").

### Fixed
- `{go_dlv_port}` was never substituted, causing `invalid hostPort: {go_dlv_port}`; it is now an environment-resolved `${go_dlv_port}` (with a backward-compat substitution for old configs).
- Compose paths resolved against the cache dir instead of the project, breaking `env_file`/mounts/build context when running `sesssions up .`.
- `sesssions init` did not propagate template updates (`up` kept using a stale global compose).