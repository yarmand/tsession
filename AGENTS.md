# AGENTS.md — Technical Reference

This document describes tsession's internal architecture for AI agents and contributors.

## Data Sources

tsession merges multiple data sources into a unified session list:

### Copilot CLI

| Path | Purpose |
|------|---------|
| `~/.copilot/session-store.db` | Recent sessions (id, summary, timestamps) |
| `~/.copilot/session-state/<uuid>/workspace.yaml` | Authoritative `cwd` per session |
| `~/.copilot/session-state/<uuid>/events.jsonl` | Live state (working / waiting / idle / exited) |
| `~/.copilot/session-state/<uuid>/inuse.<pid>.lock` | Owning Copilot PID |

### pi

| Path | Purpose |
|------|---------|
| `~/.tsession/pi-state/<uuid>.json` | State written by the pi extension (working / question / done / idle / exited) |

### tmux

Sessions are matched to tmux panes via:
1. **PID-based** (authoritative): walk the owning PID's ancestor chain until it matches a pane PID
2. **Fallback**: match `basename(cwd)` against tmux session name

Resume uses the matched `session:window.pane` target so the exact pane hosting the session is focused.

If there is no tmux match, resume falls back to `copilot --resume <id>` (Copilot) or `pi --session <id>` (pi).

## Pi Extension

To track pi session state, install the bundled extension:

```bash
cp extension/pi/tsession-state.ts ~/.pi/agent/extensions/
```

Then reload pi (or restart it). The extension writes state to `~/.tsession/pi-state/` automatically on every session lifecycle event.

## Background Cache (`watch`)

A live load typically completes in ~200 ms with ~50 sessions. For sub-10 ms reads (e.g. a tmux popup re-rendering on every keystroke), run a background watcher:

```bash
tsession watch --daemon                 # interval=10s, logs to ~/.tsession/watch.log
tsession watch --daemon --interval=5s   # custom interval
tsession stop-watch                     # stop it
```

The cache file is `~/.tsession/cache.json`. When it's within `2 × interval` of now, `list`/`browse`/`popup` use it directly. Otherwise they fall back to a live load — a crashed or stale watcher never silently lies.

Pass `--no-cache` to `list` to force a live load. The watcher is **not** auto-started; run `tsession watch --daemon` once per login session if you want the cache.

## Notifications (`--notify`)

`--notify` fires a macOS notification (via `osascript`) when a session enters
`done` (message `[name] done!`, sound "Tink") or `question` (`[name] needs your
input`, sound "Funk"). `name` is the UI label priority: `Name` → `Summary` →
`basename(cwd)`.

The `internal/notify` package diffs the current session list against a persisted
snapshot, `~/.tsession/notify.json` (map of session ID → last-notified state),
under an advisory `flock` on `~/.tsession/notify.lock`. The lock makes each
transition fire **exactly once** even when `watch --daemon --notify` and
`browse --watch --notify` observe concurrently. The first time a session ID is
seen its state is recorded silently (no notification) to avoid a startup flood.

Notifications are independent of `donestate` (which drives `done` rendering and
is consumed by every load). `browse --watch` refreshes by re-running
`tsession list --fzf --notify` every 5s, so the snapshot must be on disk rather
than in memory. macOS only — `fire` is a build-tagged no-op elsewhere.

## Sort Order

Pinned to bucket (`exited` always last; otherwise `tmux-attached` → `active no-tmux` → `idle`), then by state priority, then by recency.

## Source Indicators

| Prefix | Source |
|--------|--------|
| ©      | Copilot CLI |
| π      | pi     |

## State Machine

| Glyph | State    | Meaning |
|-------|----------|---------|
| ●     | working  | Agent is processing (Copilot: tool execution; pi: turn in progress) |
| ◐     | question | Agent finished with a question (Copilot: `ask_user`; pi: last message ends with `?`) |
| ✓     | done     | Agent finished; cleared on pane switch |
| ○     | active   | Session open, waiting for user input |
| ·     | idle     | No live process, no shutdown event |
| ·     | exited   | Session shut down |

## Commands (full reference)

```
tsession list [flags]                        # print recent sessions to stdout
tsession new <branch> [-- copilot-args]         # create worktree + tmux session, start copilot
tsession new [-p|--path <dir>] [-- copilot-args] # start a session on an existing worktree (defaults to cwd)
tsession browse [flags] [q]                  # fzf picker in current terminal
tsession popup [flags]                       # fzf picker designed for tmux popup
tsession resume [--target=..] <session-id>   # switch tmux pane (or fall back)
tsession rename <session-id> [name]          # rename a session
tsession rename-repo <session-id> [alias]    # rename a repository
tsession vscode <session-id>                 # open session directory in VS Code
tsession watch [--daemon]                    # refresh cache every --interval (default 10s)
tsession stop-watch                          # stop a running watch process
tsession serve [--addr] [--open]             # loopback web UI: session list + browser terminal
```

## New Sessions (`new`)

`tsession new <branch>` creates a git worktree, opens a tmux session named
`basename(worktree-path)`, and starts copilot in it. `tsession new -p <dir>`
(or `--path <dir>`) does the same on an existing worktree; with no branch and no
path it uses the current working directory. Anything after `--` is forwarded to
copilot.

The worktree-creation commands are configurable via `~/.config/tsession/new-worktree.sh`,
auto-created with defaults on first run. The script receives the branch name as
`$1` and must print the final worktree path as the last line of stdout. The
default creates `<repo>.worktrees/<branch>` with a `$USER/<branch>` branch.

If a tmux session with the target name already exists at the same path, `new`
resumes it; if it exists at a different path, `new` uses a unique suffixed name.

### Flags (`list`, `browse`, `popup`)

| Flag | Description |
|------|-------------|
| `--max-age <dur>` | Ignore sessions older than this (default `336h` = 14 days) |
| `--active` | Only show sessions attached to tmux whose state is neither `exited` nor `unknown` |
| `--short` | Compact rendering: state glyph, compact repository label (`[repository-or-alias]worktree` when repository data is available, otherwise the worktree basename), summary (30 chars), age suffix. |
| `--lshort <n>` | Implies `--short`; truncate each display line to `n` characters (preserves age suffix). Disables color. |
| `--no-color` | (list only) Disable ANSI colors |
| `--fzf` | (list only) Tab-delimited output for fzf consumption (display + selection ID) |
| `--no-cache` | (list only) Skip the watcher cache and load live |
| `--watch` | (browse only) Auto-refresh every 5s and re-open picker after each selection. `ESC` exits. |
| `--target <value>` | (browse, resume) Switch a different tmux client. Pass a `/dev/...` path directly, or any other value (e.g. `pick`) to choose interactively via fzf at startup. |
| `--notify` | (list, browse, watch) Fire a macOS desktop notification when a session enters `done` (sound "Tink") or `question` (sound "Funk"). Off by default. Needs a long-running observer: `watch --daemon --notify` or `browse --watch --notify`. No-op on non-macOS. |

## Session Names

Picker shortcuts:
- `ctrl-n` Rename session
- `ctrl-a` Rename repository

Sessions can be given custom display names via `ctrl-n` in the picker or `tsession rename <id> [name]`. Names are stored in `~/.tsession/names.json` and shown in the `NAME` column.

Repositories can be given shared short aliases via `ctrl-a` in the picker or `tsession rename-repo <id> [alias]`. Aliases are stored in `~/.tsession/repo-names.json` and are used in `--short` rendering.

When a session has a corresponding tmux session, renaming also renames the tmux session. To clear a name, rename with an empty string.

## Web UI (`serve`)

`tsession serve` is a loopback-only HTTP server (`internal/webui`) providing a
browser-based alternative to the tmux-split workflow, primarily to avoid
tmux-in-tmux nesting when resuming remote sessions. Full design:
`docs/superpowers/specs/2026-09-11-web-session-ui-design.md`.

**Packages:**

| Package | Responsibility |
|---|---|
| `internal/webui` | HTTP surface only: `/`, `/api/sessions`, `/api/sessions/{id}/name`, `/api/repos/alias`, `/api/events` (SSE), `/api/terminal/{origin}/{id}` (WebSocket). No tmux/git/SSH I/O of its own — everything comes from injected provider functions (`SessionsProvider`, `AliasesProvider`, `RemoteResolver`) or a `*webterm.Registry`, wired via functional options (`WithAliases`, `WithRemotes`, `WithTerminal`) on `NewServer`. |
| `internal/webterm` | Generic PTY registry keyed by `(origin, sessionID)`. Owns the `creack/pty` file, child process, a 256KB output ring buffer (replayed on reconnect), and fan-out to subscribed WebSocket clients. Knows nothing about tmux or SSH. |
| `internal/attachcmd` | The only place that knows how to build the command `webterm` runs: grouped-tmux-attach scripts, remote-transport wrapping (via `config.Remote.ResumeCommand()`), and `BuildKill` for teardown (`tmux kill-session`). |
| `internal/webui/static` | `go:embed`ed frontend: `index.html`, `app.js`, `app.css`, plus vendored `xterm.js`/`xterm.css`/the fit addon under `vendor/` (MIT-licensed, no Node build step, no CDN dependency at runtime). |

**Attach model:** local sessions attach through a **grouped tmux session**
(`tmux new-session -t <original>`, name `tsession-web-<sha256(origin+id)[:12]>`)
so the browser gets independent sizing without resizing or stealing any split
already attached to that session. Remote sessions run the identical script
through the same SSH/`gh codespace ssh`/`docker exec` transport already used by
`ensureRemoteBridge`. PTYs stay warm across tab closes; teardown
(`tmux kill-session` over the same transport) only runs on explicit close or
server shutdown (`Registry.Shutdown`), never on a WebSocket disconnect (which
only unsubscribes).

**Grouped-session filtering:** because a grouped session's panes share the
original's pane PIDs, `tmux list-panes -a` reports the same PID under both
session names. `internal/tmux` filters out `tsession-web-*` before any
PID-based `TmuxTarget` resolution, and startup reaps orphaned local
`tsession-web-*` sessions from a prior crash (`webterm.ReapOrphanedLocal`).

**Notifications:** the web path reuses `internal/notify`'s diff logic
(`notify.DiffWithStore`) against its own snapshot,
`~/.tsession/notify-web.json`, independent of the desktop path's
`~/.tsession/notify.json` — so `watch --daemon --notify` and a browser tab can
observe the same done/question transitions concurrently without racing over
one lock file. The browser renders its own `Notification`, not an `osascript`
call.

**Safety:** `--addr` must resolve to loopback (`127.0.0.1`/`127.0.0.0/8`,
`::1`, or literal `localhost`); anything else is rejected before the listener
binds. There is no auth, TLS, or non-loopback access in v1 — PTYs must never
be reachable off-host.

