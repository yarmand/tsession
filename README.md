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
tsession resume [--target=..] <session-id>  # switch tmux pane (or fall back to `copilot --resume`)
tsession rename <session-id> [name]  # rename a session (interactive if no name given)
tsession vscode <session-id>  # open session directory in VS Code
tsession watch [--daemon]     # refresh ~/.tsession/cache.json every --interval (default 10s)
tsession stop-watch           # stop the running watcher
```

### Common flags (`list`, `browse`, `popup`)

| Flag                | Description                                                                                          |
|---------------------|------------------------------------------------------------------------------------------------------|
| `--max-age <dur>`   | Ignore sessions older than this (default `336h` = 14 days).                                          |
| `--active`          | Only show sessions attached to tmux whose state is neither `exited` nor `unknown`.                   |
| `--short`           | Compact rendering: state glyph, `originLetter-basename(cwd)`, summary (30 chars), age suffix. In `browse`/`popup`, shows a right-side preview with an origin legend. |
| `--lshort <n>`      | Implies `--short`; additionally truncate each display line to `n` characters (preserves the age suffix). Disables color. |
| `--no-color`        | (list only) Disable ANSI colors.                                                                     |
| `--fzf`             | (list only) Tab-delimited output for fzf consumption (display + selection ID).                       |
| `--no-cache`        | (list only) Skip the watcher cache and load live.                                                    |
| `--local-only`      | Skip remote session gathering (useful offline or for speed).                                         |
| `--watch`           | (browse only) Auto-refresh the list every 5s and re-open the picker after each selection. `ESC` exits. |
| `--target <value>`  | (browse, resume) Switch a different tmux client instead of the current one. Pass a `/dev/...` client path directly, or any other value (e.g. `pick`) to choose interactively via fzf at startup. The chosen target is used for all subsequent selections. |

If `browse` is started outside tmux, it automatically creates (or attaches to) a tmux session named `tsession` in `$HOME` and re-runs itself inside it.

Sort order: pinned to bucket (`exited` always last; otherwise `tmux-attached` → `active no-tmux` → `idle`), then by state priority, then by recency.

## Browse keybindings

When using `browse` or `popup`, the following keybindings are available:

| Key      | Action                                                                 |
|----------|------------------------------------------------------------------------|
| `enter`  | Switch to the selected session (tmux switch-client)                    |
| `ctrl-e` | Open the session's working directory in VS Code                        |
| `ctrl-n` | Rename the session (opens in tmux popup when inside tmux)              |
| `ctrl-r` | Reload the session list                                                |
| `?`      | Show keybinding help in the preview pane                               |
| `esc`/`q`| Exit the picker                                                        |

No keybinding exits the picker except `esc`/`q` — all commands keep the list visible.

## Session names

Sessions can be given custom display names via `ctrl-n` in the picker or `tsession rename <id> [name]`. Names are stored in `~/.tsession/names.json` and shown in the `NAME` column instead of the repository/CWD path.

When a session has a corresponding tmux session, renaming also renames the tmux session to keep them in sync.

To clear a name, rename with an empty string.

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

| Glyph | State    | Meaning                                                                |
|-------|----------|------------------------------------------------------------------------|
| ●     | working  | last event was `tool.execution_start` (non-prompting tool) / `agent.processing` |
| ◐     | question | last event was `tool.execution_start` for `ask_user`/`ask_question`, or a permission request |
| ✓     | done     | session just transitioned from `working` to `active`; cleared the first time you switch to its tmux pane |
| ○     | active   | `session.db` held open by a live copilot process                       |
| ·     | idle     | no live process, no shutdown event                                     |
| ·     | exited   | `session.shutdown` event in `events.jsonl`                             |

## Remote Sessions

Display Copilot CLI sessions running on remote machines alongside your local
sessions.

### Setup

Create `~/.config/tsession/config.yaml`:

```yaml
remotes:
  # Plain SSH remote
  - name: devbox
    host: devbox.local

  # SSH with custom path
  - name: server
    host: user@server.example.com
    copilot_dir: /home/user/.copilot

  # GitHub Codespace
  - name: my-codespace
    type: codespace
    codespace: urban-broccoli-abc123

  # Dev container (Docker)
  - name: my-container
    type: devcontainer
    container: myapp_devcontainer
    user: vscode

  # Custom SSH command (advanced)
  - name: custom
    ssh_command: my-ssh-wrapper
    host: target-host
```

#### Remote types

| Type | Fields | Connect command |
|------|--------|----------------|
| `ssh` (default) | `host`, optional `ssh_command` | `ssh <host> ...` |
| `codespace` | `codespace` (name) | `gh codespace ssh --codespace <name> ...` |
| `devcontainer` | `container`, `user` | `docker exec -u <user> <container> ...` |

#### All fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | yes | — | Label shown in the section header |
| `type` | no | `ssh` | Remote type: `ssh`, `codespace`, or `devcontainer` |
| `host` | type=ssh | — | SSH destination (user@host or ssh-config alias) |
| `ssh_command` | no | `ssh` | Custom SSH binary/command (type=ssh only) |
| `codespace` | type=codespace | — | Codespace name (from `gh codespace list`) |
| `container` | type=devcontainer | — | Docker container name |
| `user` | type=devcontainer | — | User inside the container (e.g. `vscode`) |
| `copilot_dir` | no | `~/.copilot` | Path to Copilot state on the remote |

**Requirements on the remote:**
- `bash` and `sqlite3` must be available in PATH
- `tmux` (optional — enables pane-level matching)
- SSH must be configured for passwordless access (key-based auth)

### How it works

`tsession` runs a lightweight gather script over SSH that collects session data
from the remote's `~/.copilot/` directory and tmux state. Data is returned as
JSON in a single SSH round-trip. Each remote appears as its own section:

```
── Local ──────────────────────────────────────────────────────────
  ● working  2m  tsession    Fix browse layout
  ○ active   1h  myproject   Add auth module
── devbox ─────────────────────────────────────────────────────────
  ● working  5m  backend     Implement caching
  · idle     3h  infra       Terraform refactor
```

### Resume behavior

Selecting a remote session opens an interactive connection:
- **ssh**: `ssh -t <host> tmux attach -t <target>` or `copilot --resume=<id>`
- **codespace**: `gh codespace ssh --codespace <name> -t -- tmux attach -t <target>`
- **devcontainer**: `docker exec -it -u <user> <container> tmux attach -t <target>`

### Flags

| Flag           | Description                                        |
|----------------|----------------------------------------------------|
| `--local-only` | Skip remote gathering (useful offline or for speed) |

### Caching

When `tsession watch` is running, remote data is gathered alongside local data
on each refresh cycle. Each remote has a 10-second timeout — unreachable hosts
are skipped with a warning without blocking the local cache update.

### Troubleshooting

- **Remote unreachable:** The section shows as
  `── devbox (unreachable) ──` and local sessions work normally.
- **sqlite3 not found:** The remote is skipped. Install `sqlite3` on the remote.
- **Slow SSH:** Ensure `ControlMaster` is configured in `~/.ssh/config` for
  persistent connections. The gather script completes in <1s on most hosts.
