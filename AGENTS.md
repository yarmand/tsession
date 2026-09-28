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
| `--json` | (list only) Emit sessions as machine-readable JSON |
| `--no-cache` | (list only) Skip the watcher cache and load live |
| `--watch` | (browse only) Auto-refresh every 5s and re-open picker after each selection. `ESC` exits. |
| `--target <value>` | (browse, resume) Switch a different tmux client. Pass a `/dev/...` path directly, or any other value (e.g. `pick`) to choose interactively via fzf at startup. |
| `--notify` | (list, browse, watch) Fire a macOS desktop notification when a session enters `done` (sound "Tink") or `question` (sound "Funk"). Off by default. Needs a long-running observer: `watch --daemon --notify` or `browse --watch --notify`. No-op on non-macOS. |

Remote discovery and attachment initialize the configured remote shell as an
interactive login shell, so user PATH setup such as Homebrew is available.
Gathering prefers the resulting PATH-resolved `tsession` without installation
or version checks. If absent, it installs a matching release. It then ensures
`tsession watch --daemon` is running and consumes
`tsession list --active --local-only --json`, preserving the same tmux
session/pane match shown by an interactive remote list.

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
| `internal/webui` | HTTP surface only: `/`, `/api/sessions`, `/api/sessions/{id}/name`, `/api/repos/alias`, `/api/events` (SSE), `/api/terminal/{origin}/{id}` (WebSocket), `/api/codeserver/{origin}/{id}` (start/status/stop), `/api/code/{key}/...` (reverse proxy). No tmux/git/SSH I/O of its own — everything comes from injected provider functions (`SessionsProvider`, `AliasesProvider`, `RemoteResolver`, `CodeConfigProvider`) or a `*webterm.Registry`/`*codeserver.Registry`, wired via functional options (`WithAliases`, `WithRemotes`, `WithTerminal`, `WithCodeServer`) on `NewServer`. |
| `internal/webterm` | Generic PTY registry keyed by `(origin, sessionID)`. Owns the `creack/pty` file, child process, a 256KB output ring buffer (replayed on reconnect), and fan-out to subscribed WebSocket clients. Knows nothing about tmux or SSH. |
| `internal/attachcmd` | The only place that knows how to build the command `webterm` runs: grouped-tmux-attach scripts, remote-transport wrapping (via `shellutil.WrapRemoteTransport`), and `BuildKill` for teardown (`tmux kill-session`). |
| `internal/codecmd` | Pure command builder (no process I/O) for the code view: `Key(origin, id)` (opaque URL key), `Build(...)` (the `code serve-web` launch command, local or remote-transport-wrapped), and `TunnelCommand(...)` (ssh -L / `gh codespace ports forward` for reaching a remote instance's port). |
| `internal/codeserver` | Registry of long-lived `code serve-web` child processes keyed by `(origin, sessionID)`, mirroring `internal/webterm`'s architecture: PTY-backed launch (so remote transports needing a tty work and stdout+stderr combine into one stream), ANSI-stripped port-scanning of the "Web UI available at" line, a bounded log ring, and per-transport dialing (`LocalDialer`, `RemoteTunnelDialer` for ssh/codespace, `DevcontainerDialer` for a per-connection `docker exec -i` stdio relay since `docker exec` has no port-forwarding primitive). `Registry.Start` is idempotent while an instance is starting/running — this is what makes reactivating a session with an existing code view reuse it instead of relaunching. |
| `internal/webui/static` | `go:embed`ed frontend: `index.html`, `app.js`, `app.css`, plus vendored `xterm.js`/`xterm.css`/the fit addon under `vendor/` (MIT-licensed, no Node build step, no CDN dependency at runtime). |

The web/GUI session list uses per-remote label colors. `Alt+H` (or the
top-left button) toggles the persistent sidebar. If collapsed, `Alt+/` opens it
as an overlay over the terminal; selecting a session closes the overlay without
resizing the terminal pane. The sidebar's right-edge pointer handle updates a
bounded CSS width and persists it in browser local storage.

`Alt+/` toggles focus list ↔ terminal. While focus is inside the VS Code code
pane it always moves focus to the terminal. Keydowns inside that same-origin
iframe never bubble to the parent document, so `static/codekeys.js` hooks the
frame's own window (re-attached on every iframe `load`) in the **capture**
phase and calls `focusTerminal()`, consuming the chord before VS Code sees it.

**Row layout:** each row reads `glyph source location worktree repository
age`. The **worktree** folder (`SessionView.Worktree`, i.e.
`render.WorktreeName`) leads and never shrinks, because sessions on several
worktrees of one repository are otherwise indistinguishable; the repository
label trails it, dimmed, and absorbs all truncation. As in `--short`
rendering, the repository token is omitted when it is identical to the
worktree name. A custom session name replaces the worktree in the row
(rendered in italics); the folder itself stays visible in the row tooltip and
in the `Worktree` row of the session-info panel.

**Picker shortcuts** (sidebar focus only, so an attached terminal still
receives these keys):

| Key | Action |
|---|---|
| `ctrl-n` | Rename the session under the cursor (`POST /api/sessions/{id}/name`) |
| `ctrl-a` | Alias the repository under the cursor (`POST /api/repos/alias`) |
| `F2` | Same as `ctrl-n` |

Double-clicking a row renames it; right-clicking the repository token sets an
alias. Any edit to `internal/webui/static/*` must bump `CACHE_NAME` in
`sw.js`, or the service worker keeps serving the previous app shell.

**Session-list cache and fast reattach:** for remote sessions, the
`SessionsProvider` does a live SSH round trip (`internal/remote.FetchAll`),
which can take ~2s — long enough that switching sessions used to visibly
block on it. `internal/webui/sessioncache.go` wraps it in a
stale-while-revalidate cache (3s TTL, `Server.SetSessionTTL` as a 0-disables
test seam) shared by `/api/sessions`, `/api/events`'s poller, rename, and the
terminal handler's session lookup: a fresh read returns instantly, and a
stale read returns the last snapshot immediately while kicking off one
coalesced background refresh. Renaming patches the cached entry's `Name` in
place (`sessionCache.PatchName`) so it shows up instantly without waiting out
the TTL or forcing a reload; repository aliases bypass the cache entirely
(resolved per-request via `aliasesFn`). Separately, `handleTerminal` first
checks `webterm.Registry.Live` for an already-warm PTY and, if found,
upgrades and subscribes immediately — skipping the session lookup (and, for
remote sessions, needing the remote to be reachable at all) whenever you're
reattaching to a session you've already opened.

**Per-session terminal panes:** the client keeps one xterm instance per
session (`state.panes`, keyed by session key, LRU-capped at 8) rather than
resetting and replaying a single shared terminal on every switch. Switching
sessions just hides the previous pane's DOM node and shows the target's, so
scrollback and viewport position survive; only the visible pane is ever
`fit()`. Evicting a pane over the cap just closes its WebSocket — the
server-side PTY (`webterm.Registry`) stays warm regardless, so a later
re-attach replays its ring buffer rather than starting over. A small
"Connecting…" overlay shows per-pane while its socket is still opening.

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

**Clipboard (OSC 52):** copying inside the browser terminal — including from
a *remote* session's tmux copy-mode — must reach the local system clipboard.
tmux (with the default `set-clipboard external`) reports its own copies by
emitting an OSC 52 escape to the attached client, but **xterm.js has no
built-in OSC 52 handler** (the vendored bundle registers only `0,1,2,4,8,
10,11,12,104,110,111,112`). `static/clipboard.js` adds one via
`term.parser.registerOscHandler(52, ...)`:

- `decode(payload)` parses `<targets>;<base64>`. A `?` payload is a *read
  query* and is deliberately never answered, so a remote host can never read
  the local clipboard. An empty payload is a no-op so the clipboard is not
  wiped.
- `systemWriter(navigator, document)` tries `navigator.clipboard.writeText`
  first and falls back to a hidden `<textarea>` + `document.execCommand("copy")`.
  The fallback is **required**, not defensive: the async Clipboard API needs
  transient user activation, which an escape sequence arriving over a
  WebSocket never has, so it rejects with `NotAllowedError`.
- The handler always returns `true` so the sequence is consumed rather than
  printed into the terminal.

Failures report `terminal-clipboard-failed` through the `/api/debug` channel.

tmux only emits OSC 52 for xterm-compatible `TERM` values (verified: works for
`xterm`, `xterm-256color`, `tmux-256color`; silent for `screen-256color`, and
nothing is emitted with an empty or `dumb` TERM). Because a GUI launched from
Finder/Dock inherits no `TERM` at all, `webterm.terminalEnv()` strips any
inherited `TERM` and pins `TERM=xterm-256color` on the PTY child — which is
accurate anyway, since the far end is an xterm.js emulator.

**Safety:** `--addr` must resolve to loopback (`127.0.0.1`/`127.0.0.0/8`,
`::1`, or literal `localhost`); anything else is rejected before the listener
binds. There is no auth, TLS, or non-loopback access in v1 — PTYs must never
be reachable off-host.

### Code view (`Alt+E`)

An optional VS Code (`code serve-web`) pane, docked to the right of the
terminal via a resizable divider, toggled with `Alt+E` while the session list
is focused. Full design: `docs/superpowers/specs/2026-09-18-code-view-design.md`.

It mirrors the terminal's own three-layer split — `internal/codecmd` (pure
command building) → `internal/codeserver` (process registry) →
`internal/webui`'s `/api/codeserver/*` and `/api/code/{key}/...` routes — so
each layer is unit-testable without a live `code` binary or remote host.

**Reaching the instance:** `code serve-web` is launched with
`--server-base-path /api/code/<key>` (where `key = sha256(origin + id)[:6]`
hex-encoded), so it emits URLs already prefixed with that path. `/api/code/{key}/...`
is a same-origin `httputil.ReverseProxy` whose `Transport.DialContext` always
calls the target `codeserver.Instance.Dial()` regardless of the requested
address — this is what lets the same proxy code serve a direct loopback dial
(local), an established ssh/codespace tunnel's local end (`RemoteTunnelDialer`),
or a fresh per-connection `docker exec` stdio relay (`DevcontainerDialer`)
uniformly. The request path is forwarded unmodified (no path rewriting), which
is why `--server-base-path` must match `/api/code/<key>` exactly.

**`--server-data-dir` is per host, not per tsession.** A local instance gets
`~/.tsession/codeserver/<key>` from `webui`'s `codeDataDir`. A *remote*
instance must not receive that path: the remote has its own filesystem and
user, so a macOS-shaped path like `/Users/...` cannot be created on a Linux
host. VS Code does not fall back — its extension host dies with
`EACCES: permission denied, mkdir '/Users'`, and with no extension host
nothing extension-backed works, including GitHub sign-in. `codecmd.Build`
therefore ignores the caller's `dataDir` for remote sessions and substitutes
`codecmd.RemoteDataDir(key)`, an **unquoted** `"$HOME/.tsession/codeserver/<key>"`
expanded by the remote shell, with a `mkdir -p` ahead of the exec (VS Code
will not create missing parents).

**Per-session, not per-connection:** `state.codeViews` in `app.js` keeps one
retained `<iframe>` per session key, created once and only ever shown/hidden
— never destroyed or reloaded — so switching sessions preserves editor state
and switching back is instant. Only the pane's ✕ button issues `DELETE
/api/codeserver/{origin}/{id}`, which is the only thing that stops VS Code;
switching sessions just hides the pane. `POST /api/codeserver/{origin}/{id}`
(re)starts or reuses (never relaunches, per `codeserver.Registry.Start`'s
idempotency) the session's instance; `GET` reports `starting`/`running`/
`failed`/`stopped` plus a captured log tail, polled by the frontend while
starting (VS Code's first run downloads the server build and can take
minutes).

**Sign-in popups:** `internal/webui/static/external.js` intercepts popup
requests from the same-origin VS Code iframe. `gui/frontend/dist/index.html`
marks the native GUI URL with `?gui=1`, since Wails' macOS WebKit does not
create popup windows; native GUI links go to the default browser instead.

VS Code never opens the sign-in URL directly. While the user gesture is still
active it *reserves* a blank popup with an argument-less `window.open()`, and
only assigns `location.href` once the auth extension has produced the URL. If
the reservation yields no window, VS Code abandons the flow and Settings Sync
sign-in times out. The bridge therefore always returns a stand-in window for a
reservation, and hands the eventually assigned URL to the host browser. In an
ordinary browser the real popup is still attempted first; `noopener` is
stripped from the feature list (and re-applied to the result) so a `null`
return reliably means "blocked" rather than "succeeded silently".

`/api/open-external` only accepts same-origin JSON requests addressed to a
loopback host and HTTPS (or loopback HTTP) URLs; debug logs omit URL query
strings to avoid leaking OAuth tokens. The iframe allows clipboard access for
sign-in codes.

Changing any `static/` asset requires bumping `CACHE_NAME` in `static/sw.js`,
or the service worker keeps serving the previous copy.

**Config:** the `code` binary is resolved the same way as the terminal
attach's `copilot`/`pi` resolution — top-level `code_command` or a
per-remote `code_command` override in `~/.config/tsession/config.yaml`
(`config.Remote.CodeBinary`), otherwise `code` from PATH (locally via
`exec.LookPath`, remotely via `shellutil.CodeResolverCommand()` sourcing the
remote's own interactive login shell).

**Debug logging:** `POST /api/debug` (`internal/webui/debug.go`) lets the
browser report user interactions (session selection, focus changes,
sidebar/code-pane resize, code-view show/hide/close) and its own detected
failures through `Server.logInteraction`, which writes one line per event to
the `serve` process log (`serve user interaction: action=... session=...
origin=... level=... name="..." detail="..."`, default `level=info` via
`log.Printf`, overridable with `SetDebugLog` in tests). The server also logs
failures it detects independently of the browser — every error branch in
`handleCodeServerStart/Status/Stop`, plus `writeCodeServerResponse` logging
`code-view-instance-failed` (with the instance's captured log tail) whenever
it observes `codeserver.StatusFailed`, sync or async. Every ssh (or
equivalent) command actually sent to a remote is also logged before it
runs, regardless of whether it later succeeds: `code-view-ssh-command` (the
launch command built by `codecmd.Build`, logged in `handleCodeServerStart`),
`code-view-tunnel-ssh-command` (the `ssh -L`/`gh codespace ports forward`
tunnel built by `codecmd.TunnelCommand`, logged via the `CommandLogFunc`
threaded through `codeserver.PortReadyFor`/`RemoteTunnelDialer`), and
`terminal-ssh-command` (the attach command built by `attachcmd.Build`,
logged in `handleTerminal`) — each with the full shell-quoted command line
as `detail`, so a misconfigured `code_command` or unreachable host is
diagnosable from the exact command that was invoked. `grep level=error`
finds every failure from either side; `grep ssh-command` finds every ssh
invocation.
