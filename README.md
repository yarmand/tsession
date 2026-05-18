# tsession

Manage [Copilot CLI](https://github.com/github/copilot-cli) sessions from tmux.

`tsession` joins three data sources:
- `~/.copilot/session-store.db` — recent sessions (id, cwd, summary)
- `~/.copilot/session-state/<uuid>/events.jsonl` — live state (working / waiting / idle / exited)
- `tmux list-sessions` — matches sessions by `basename(cwd)` to tmux session name

…and surfaces them in a tmux popup picker that switches your tmux client to the matching session on Enter.

## Install

Requires Go 1.25+, `tmux`, `fzf`, `lsof`.

```bash
make install            # builds and installs to ~/.local/bin/tsession
```

## Usage

```bash
tsession list                 # print recent sessions to stdout
tsession browse [query]       # fzf picker in current terminal
tsession popup                # fzf picker designed for tmux popup
tsession resume <session-id>  # switch tmux or fall back to `copilot --resume`
tsession watch [--daemon]     # refresh ~/.tsession/cache.json every --interval (default 10s)
tsession stop-watch           # stop the running watcher
```

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
`--no-cache` to `list` to force a live load.

## tmux popup keybind

Add to `~/.tmux.conf`:

```tmux
bind -n M-s display-popup -E -w 90% -h 70% "tsession popup"
```

Then `Alt-s` opens the picker from any tmux pane.

## State legend

| Glyph | State    | Meaning                                                    |
|-------|----------|------------------------------------------------------------|
| ●     | working  | last event was `tool.execution_start` / `agent.processing` |
| ◐     | waiting  | last event was `ask_question` / `permission_request`       |
| ○     | active   | `session.db` held open by a live copilot process           |
| ·     | idle     | no live process, no shutdown event                         |
| ·     | exited   | `session.shutdown` event in `events.jsonl`                 |
