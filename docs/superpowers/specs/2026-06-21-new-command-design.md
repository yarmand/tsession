# `tsession new` — Design

## Summary

A new subcommand, `tsession new`, that creates (or reuses) a git worktree, opens
a tmux session in it, and starts Copilot CLI there. The commands used to create
the worktree are configurable via a standalone shell script that tsession
auto-creates with sensible defaults on first run.

## CLI Interface

```
tsession new <branch> [-- <copilot-args>...]      # create a worktree for <branch>, then start a session
tsession new --path <dir> [-- <copilot-args>...]  # start a session on an existing worktree at <dir>
```

Rules:

- `<branch>` (positional) and `--path <dir>` are **mutually exclusive**; exactly
  one must be provided. Providing both, or neither, is a usage error.
- Everything after a literal `--` is forwarded verbatim to `copilot` when it is
  launched in the tmux session.
- The tmux session name is `basename(worktree-path)`.

## Worktree-Creation Script (configurable)

- Location: `~/.config/tsession/new-worktree.sh`.
- **Auto-created on first run** if it does not exist, with mode `0755` and the
  default content below. An existing file is never overwritten.

Default script:

```sh
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(git rev-parse --git-common-dir)/.." && pwd)"
wt_folder="${repo_root}.worktrees"
mkdir -p "$wt_folder"
wt_path="$(realpath "$wt_folder")/$1"
git worktree add -b "$USER/$1" "$wt_path"
echo "$wt_path"
```

### Execution contract

- Invoked as `bash <script-path> <branch>` with the **current working directory**
  inherited (the user is expected to run `tsession new` from inside the target
  git repository — the default script relies on `git rev-parse`).
- **stderr** is streamed to the user so git/worktree progress is visible.
- **stdout** is captured; the **last non-empty line of stdout is the resulting
  worktree path**. If stdout has no non-empty line, or the script exits non-zero,
  `new` aborts with an error and does not create a tmux session.

This contract is the only coupling between tsession and the script, so users can
freely rewrite the script as long as it prints the final worktree path last.

## tmux Session Creation & Collision Handling

After resolving the worktree path (from the script for `<branch>`, or directly
from `--path`):

1. Compute `name = basename(path)`.
2. Inspect existing tmux sessions (name + path):
   - **Name exists and its path equals the target path** → do **not** recreate;
     switch/attach to the existing session (resume behavior).
   - **Name exists but path differs** → choose a unique suffixed name
     (`name-2`, `name-3`, …) and create a fresh session under that name.
   - **Name does not exist** → use `name` as-is.
3. Create the session detached and start Copilot in it:
   `tmux new-session -d -s <name> -c <worktree-path> "copilot <extra-args...>"`.
4. Switch/attach to the session: inside tmux → `switch-client`; outside tmux →
   `attach-session` (reusing the existing `tmux.SwitchClientTarget`).

## Components

- `cmd/new.go` — `New(args []string) error`. Flag parsing, mutual-exclusion
  validation, `--`-args splitting, and orchestration of worktree + tmux steps.
- `main.go` — wire the `new` subcommand into the dispatch switch and `usage()`.
- `internal/worktree/worktree.go`:
  - `ScriptPath() string` — resolves `~/.config/tsession/new-worktree.sh`.
  - `EnsureScript() error` — writes the default script (0755) if absent.
  - `Create(branch string) (path string, err error)` — ensures the script,
    runs it with the branch, streams stderr, captures stdout, returns the
    last non-empty stdout line.
  - Default-script constant. The script path is overridable (param/var) for tests.
- `internal/tmux/tmux.go` — add:
  - `NewSession(name, path, command string) error` — runs detached `new-session`.
  - A testable helper to resolve the final session name given the desired name,
    target path, and the current session list (returns the existing target to
    resume, or a unique new name). Built on the existing `parseListSessions`.

## Error Handling

- Missing/invalid args (both or neither of branch/`--path`) → usage error.
- Worktree script exits non-zero or prints no path → abort before any tmux work,
  surfacing the script's stderr.
- `--path` pointing at a non-existent directory → error before tmux work.

## Testing

- `worktree`: `EnsureScript` writes the default only when the file is absent and
  preserves an existing file; `Create` returns the last non-empty stdout line
  using a stub script (script path injected).
- `tmux`: unique-name/resume resolution given a synthetic session list;
  `new-session` argument construction via a pure builder function (no tmux
  required).
- `cmd/new`: flag validation and `--` splitting via a pure helper.

## Out of Scope (YAGNI)

- Configurable Copilot launch command (only worktree creation is configurable;
  extra args are forwarded after `--`).
- Non-Copilot agents (e.g. pi) for `new`.
- Remote worktree/session creation.
