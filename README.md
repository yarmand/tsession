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
