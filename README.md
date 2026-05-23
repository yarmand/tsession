# tsession

Manage [Copilot CLI](https://github.com/github/copilot-cli) sessions from tmux.

`tsession` joins four data sources:
- `~/.copilot/session-store.db` — recent sessions (id, summary, timestamps)
- `~/.copilot/session-state/<uuid>/workspace.yaml` — authoritative `cwd` per session
- `~/.copilot/session-state/<uuid>/events.jsonl` — live state (working / waiting / idle / exited)
- `~/.copilot/session-state/<uuid>/inuse.<pid>.lock` — owning Copilot PID
- `tmux list-sessions` / `tmux list-panes` — matches sessions to tmux panes:
  1. PID-based: walk the owning PID's ancestor chain until it matches a pane PID (authoritative)
  2. Fallback: match `basename(cwd)` against tmux session name

Resume uses the matched `session:window.pane` target so the exact pane hosting the Copilot session is focused, not just the tmux session.

The picker switches your tmux client to the matching pane on Enter; if there is no tmux match, it falls back to `copilot --resume <id>`.

## Install

Requires Go 1.25+, `tmux`, `fzf`, `lsof`.

```bash
make install            # builds and installs to ~/.local/bin/tsession
```

## Usage

```bash
tsession list [flags]         # print recent sessions to stdout
tsession browse [flags] [q]   # fzf picker in current terminal
tsession popup [flags]        # fzf picker designed for tmux popup
tsession resume <session-id>  # switch tmux pane (or fall back to `copilot --resume`)
tsession watch [--daemon]     # refresh ~/.tsession/cache.json every --interval (default 10s)
tsession stop-watch           # stop the running watcher
```

### Common flags (`list`, `browse`, `popup`)

| Flag                | Description                                                                                          |
|---------------------|------------------------------------------------------------------------------------------------------|
| `--max-age <dur>`   | Ignore sessions older than this (default `336h` = 14 days).                                          |
| `--active`          | Only show sessions attached to tmux whose state is neither `exited` nor `unknown`.                   |
| `--short`           | Compact rendering: `state`, `age`, `basename(cwd)`, first 30 chars of summary.                       |
| `--lshort <n>`      | Implies `--short`; additionally truncate each line to `n` characters. Disables color.                 |
| `--no-color`        | (list only) Disable ANSI colors.                                                                     |
| `--fzf`             | (list only) Tab-delimited output for fzf consumption (display + selection ID).                       |
| `--no-cache`        | (list only) Skip the watcher cache and load live.                                                    |
| `--watch`           | (browse only) Auto-refresh the list every 5s and re-open the picker after each selection. `ESC` exits. |

Sort order: pinned to bucket (`exited` always last; otherwise `tmux-attached` → `active no-tmux` → `idle`), then by state priority, then by recency.

## Background cache (`watch`)

A live load typically completes in well under 300 ms (≈200 ms with
~50 recent sessions), so `list`/`browse`/`popup` are snappy without any
extra setup. For sub-10 ms reads — e.g. a tmux popup that re-renders on
every keystroke — run a background watcher that maintains a cache file:

```bash
tsession watch --daemon                 # interval=10s, logs to ~/.tsession/watch.log
tsession watch --daemon --interval=5s   # custom interval
tsession stop-watch                     # stop it
```

When the cache file at `~/.tsession/cache.json` is within `2 × interval` of
now, `list`/`browse`/`popup` use it directly. Otherwise they fall back to a
live load, so a crashed or stale watcher never silently lies. Pass
`--no-cache` to `list` to force a live load. The watcher is **not**
auto-started; run `tsession watch --daemon` once per session if you want
the cache.

## tmux popup keybind

Add to `~/.tmux.conf`:

```tmux
bind -n M-s display-popup -E -w 90% -h 70% "tsession popup --active --short"
```

Then `Alt-s` opens the picker from any tmux pane. Drop `--active --short`
for the full session list.

## State legend

| Glyph | State    | Meaning                                                    |
|-------|----------|------------------------------------------------------------|
| ●     | working  | last event was `tool.execution_start` / `agent.processing` |
| ◐     | waiting  | last event was `ask_question` / `permission_request`       |
| ○     | active   | `session.db` held open by a live copilot process           |
| ·     | idle     | no live process, no shutdown event                         |
| ·     | exited   | `session.shutdown` event in `events.jsonl`                 |
