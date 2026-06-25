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
tsession new <branch> [-- copilot-args]      # create worktree + tmux session, start copilot
tsession new --path <dir> [-- copilot-args]  # start a session on an existing worktree
tsession browse [flags] [q]                  # fzf picker in current terminal
tsession popup [flags]                       # fzf picker designed for tmux popup
tsession resume [--target=..] <session-id>   # switch tmux pane (or fall back)
tsession rename <session-id> [name]          # rename a session
tsession vscode <session-id>                 # open session directory in VS Code
tsession watch [--daemon]                    # refresh cache every --interval (default 10s)
tsession stop-watch                          # stop a running watch process
```

## New Sessions (`new`)

`tsession new <branch>` creates a git worktree, opens a tmux session named
`basename(worktree-path)`, and starts copilot in it. `tsession new --path <dir>`
does the same on an existing worktree. Anything after `--` is forwarded to
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
| `--short` | Compact rendering: state glyph, `originLetter-basename(cwd)`, summary (30 chars), age suffix. In `browse`/`popup`, shows a right-side preview with an origin legend. |
| `--lshort <n>` | Implies `--short`; truncate each display line to `n` characters (preserves age suffix). Disables color. |
| `--no-color` | (list only) Disable ANSI colors |
| `--fzf` | (list only) Tab-delimited output for fzf consumption (display + selection ID) |
| `--no-cache` | (list only) Skip the watcher cache and load live |
| `--watch` | (browse only) Auto-refresh every 5s and re-open picker after each selection. `ESC` exits. |
| `--target <value>` | (browse, resume) Switch a different tmux client. Pass a `/dev/...` path directly, or any other value (e.g. `pick`) to choose interactively via fzf at startup. |
| `--notify` | (list, browse, watch) Fire a macOS desktop notification when a session enters `done` (sound "Tink") or `question` (sound "Funk"). Off by default. Needs a long-running observer: `watch --daemon --notify` or `browse --watch --notify`. No-op on non-macOS. |

## Session Names

Sessions can be given custom display names via `ctrl-n` in the picker or `tsession rename <id> [name]`. Names are stored in `~/.tsession/names.json` and shown in the `NAME` column.

When a session has a corresponding tmux session, renaming also renames the tmux session. To clear a name, rename with an empty string.
