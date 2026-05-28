# tsession

A session navigator for [Copilot CLI](https://github.com/github/copilot-cli) and [pi](https://github.com/earendil-works/pi-mono) — browse, switch, and monitor your AI coding sessions from tmux.

## Install

Requires Go 1.25+, `tmux`, `fzf`, `lsof`.

```bash
make install    # builds and installs to ~/.local/bin/tsession
```

## Browse — session navigation in a terminal split

The primary workflow: use your terminal's native split to create two panes side by side. The left pane runs tsession as a persistent navigator; the right pane has a tmux client where your sessions live.

![browse](browse.png)

Before yo start tsession, use your native terminal split capabilities. the the split you want to display session, start a tmux session. This session is only here the tsession to easily discover the TTY.
In the split youwant the navigation, start tsession with:
```bash
tsession browse --watch --active --short --target pick
```

On first launch, tsession asks you to pick which tmux client to target (the right pane). Then it shows a live-updating fzf list of active sessions. Press `enter` to switch the target pane to that session.

The `--watch` flag keeps the picker open and refreshes every 5 seconds — it acts as a persistent session dashboard. Press `esc` to quit.

If started outside tmux, browse auto-creates a tmux session named `session-nav` and re-runs inside it.

## Popup — quick switcher from any tmux pane

For quick access without a dedicated split, bind tsession as a tmux popup:

![popup](popup.png)

Add to `~/.tmux.conf`:

```tmux
bind -n M-s display-popup -E -w 90% -h 70% "tsession popup --active --short"
```

Then `Alt-s` opens the picker as an overlay from any pane. Select a session and the popup closes, switching you there.

## Keybindings

| Key | Action |
|-----|--------|
| `enter` | Switch to the selected session |
| `ctrl-e` | Open session directory in VS Code |
| `ctrl-n` | Rename the session |
| `ctrl-r` | Reload the session list |
| `?` | Show help in the preview pane |
| `esc`/`q` | Exit the picker |

## Source indicators

| Prefix | Source |
|--------|--------|
| © | Copilot CLI |
| π | pi |

## State indicators

| Glyph | Meaning |
|-------|---------|
| ● | Agent is processing |
| ◐ | Agent finished with a question |
| ✓ | Agent finished (cleared on pane switch) |
| ○ | Waiting for user input |
| · | Idle or exited |

---
See [AGENTS.md](AGENTS.md) for technical internals, full flag reference, and cache architecture.


# Coming soon

- remote sessions support
  - ssh
  - github codespaces
  - devcontainers

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
  - name: devbox          # Section label in the picker
    host: devbox.local    # SSH host (as in ~/.ssh/config or user@host)
  - name: server
    host: user@server.example.com
    copilot_dir: /home/user/.copilot  # Optional, defaults to ~/.copilot
```

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

Selecting a remote session opens an interactive SSH connection:
- If the session is attached to a tmux pane: `ssh -t <host> tmux attach -t <target>`
- Otherwise: `ssh -t <host> copilot --resume=<id>`

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
