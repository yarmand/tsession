# Web session UI design

**Date:** 2026-09-11
**Status:** Approved for planning

## Goal

Provide a browser-based alternative to the tmux-split workflow so that resuming a
remote session does not require nesting one tmux server's client inside another.

Today, resuming a remote session creates a **local** tmux bridge session whose pane
runs `ssh -t <host> 'tmux attach-session -t …'` (`cmd/remote_bridge.go:ensureRemoteBridge`,
`internal/tmux/bridge.go:EnsureBridge`). The result is two tmux servers stacked in one
terminal: prefix-key ambiguity, doubled status bars, and broken copy-mode.

A browser tab can act as the outer terminal instead. With the browser holding the PTY
directly — via a WebSocket rather than a local tmux pane — only one tmux server remains
in the path for both local and remote sessions.

## User-visible behavior

`tsession serve` starts a local web server and opens (or prints) a URL. The page has
two panels:

- **Left, 20% width** — the session list, equivalent to `tsession browse --watch
  --active --short`: state glyph, source prefix, compact repository/worktree label,
  summary, age. Refreshes every 5 seconds.
- **Right, 80% width** — an xterm.js terminal. Clicking a row in the left panel attaches
  the right panel to that session's PTY. Switching rows switches which PTY the terminal
  displays; the previous PTY keeps running in the background.

Local sessions attach through a **grouped tmux session** so the browser gets its own
size independent of any tmux client you already have attached to the same session in a
native terminal — the web UI does not steal or resize your existing split. Remote
sessions run the identical attach script through the existing SSH / `gh codespace ssh` /
`docker exec` transports already configured in `~/.config/tsession/config.yaml`.

Double-clicking (or `F2` on) a row opens a rename modal for the session name; a
separate action renames the repository alias, mirroring `ctrl-n`/`ctrl-a` in the fzf
picker. Browser notifications fire on `done`/`question` transitions, independent of the
existing macOS desktop notifications.

The server binds `127.0.0.1` only. A non-loopback `--addr` is rejected at startup with
an explicit error — PTYs must never be reachable off-host.

## Non-goals (v1)

- `--daemon` / background server lifecycle (foreground only for now).
- VS Code open, filter toggles, search box, and `tsession new` from the web UI.
- Modifying `ensureRemoteBridge`, `EnsureBridge`, or any fzf picker behavior — the CLI
  path is untouched and remains fully functional.
- TLS, auth tokens, or any access beyond loopback.
- Rendering distinguishable tmux panes/windows natively (a `tmux -CC` control-mode
  client); the browser terminal is a plain screen-scraped attach, same as any tmux
  client.

## Verified tmux behavior

Probed on an isolated socket (`tmux -L tsession-probe`, tmux 3.6a) because the design
depends on precise grouped-session semantics:

1. `tmux new-session -d -s <web> -t <orig>` creates a **grouped** session that shares
   the original's windows and pane PIDs, and once a client attaches to it, it renders at
   its own size — the original session is not resized.
2. **`tmux list-panes -a` reports the same pane PID under both session names.** Because
   `internal/sessions/tmuxmatch.go:ResolveTmuxByPIDWithTree` builds a PID-keyed
   `paneByPID` map, a web-grouped pane can silently overwrite a session's real
   `TmuxTarget` if not filtered out. This is treated as a hazard requiring explicit
   filtering, not an edge case.
3. `tmux new-session -A` **attaches** when the session already exists and ignores `-d`.
   It is not a safe "ensure a detached session exists" primitive. The correct pattern is
   `tmux has-session -t <web> 2>/dev/null || tmux new-session -d -s <web> ...` followed
   by an explicit `attach-session`.
4. `tmux kill-session -t <web>` on a grouped session leaves the original session alive
   with its scrollback intact — teardown of a web-grouped session is safe.

## Architecture

### New packages

**`internal/webui`** — HTTP surface only; no session logic of its own.

| Route | Method | Purpose |
|---|---|---|
| `/` and static assets | GET | `go:embed`ed bundle: HTML, CSS, JS, vendored xterm.js |
| `/api/sessions` | GET | Grouped session list as JSON |
| `/api/sessions/{id}/name` | POST | Rename a session (empty body clears the name) |
| `/api/repos/alias` | POST | Set or clear a repository alias |
| `/api/events` | GET | Server-Sent Events stream of done/question notifications |
| `/api/terminal/{origin}/{id}` | GET (WS upgrade) | PTY byte stream + resize control frames |

**`internal/webterm`** — the PTY registry. Keyed by `(origin, sessionID)`. Each entry
owns:

- the `creack/pty` file and child process,
- a bounded 256 KB output ring buffer (for replaying on reconnect),
- the set of currently-subscribed WebSocket clients.

Public surface: `Attach(key, spec) (*Terminal, error)`, `Terminal.Subscribe(w)`,
`Terminal.Write([]byte)`, `Terminal.Resize(rows, cols)`, `Close(key)`. This package knows
nothing about tmux, SSH, or session metadata — it is handed a command to run and manages
its lifecycle generically.

**`internal/attachcmd`** — the one place that knows how to build the command
`webterm` runs. Absorbs the reusable shell-quoting and copilot-resolution helpers
currently private to `cmd/remote_bridge.go` (`shellQuote`, `shellJoin`,
`remoteCopilotResolverCommand`) so both the CLI bridge path and the new web path share
one implementation rather than duplicating shell-construction logic.

### Attach command construction

Web session name: `tsession-web-<first 12 hex chars of sha256(origin + "\x00" + sessionID)>`.

For a session **with** a known tmux target:

```sh
tmux has-session -t <web> 2>/dev/null || tmux new-session -d -s <web> -t <origSession>
tmux select-window -t <web>:<win>
tmux select-pane  -t <web>:<win>.<pane>
exec tmux attach-session -t <web>
```

For a session **without** a tmux target, wrap the resume command so the process
survives a server restart, mirroring the existing `remoteFallbackTmuxName` behavior on
the remote-bridge path:

```sh
tmux has-session -t <web> 2>/dev/null || tmux new-session -d -s <web> '<resume-cmd>'
exec tmux attach-session -t <web>
```

`<resume-cmd>` is `copilot --resume=<id>` for Copilot sessions or `pi --session <id>`
for pi sessions. On remote hosts, the copilot binary path is resolved using the existing
`remoteCopilotResolverCommand()` logic. When `RemoteTmuxAvailable` is false, the resume
command runs directly with no tmux wrapper at all — matching the direct-resume fallback
already used by `remoteSessionShellCommand`.

Local sessions run this script directly in the PTY. Remote sessions run the identical
script wrapped by the existing `config.Remote.ResumeCommand()`:

- ssh: `ssh -t <host> bash -lc '<script>'`
- codespace: `gh codespace ssh --codespace <cs> -t -- bash -lc '<script>'`
- devcontainer: `docker exec -it <ctr> bash -lc '<script>'`

No new transport code is required; `attachcmd` only supplies a different inner command
than `cmd/remote_bridge.go` does today.

### PTY lifecycle

- PTYs stay warm when a browser tab closes or a different row is selected — this is a
  deliberate choice to keep remote SSH connections established and scrollback available
  for instant switching, matching how the existing local bridge sessions behave.
- A PTY is torn down on explicit user action or server shutdown. Teardown runs
  `tmux kill-session -t <web>` over the same transport used to create it, which the
  probed behavior above confirms leaves the real session untouched.
- On server startup, locally-owned orphaned `tsession-web-*` tmux sessions from a prior
  crash are reaped. Remote orphans are left alone — the deterministic naming means the
  next attach simply reuses them.
- Resize is a JSON control frame, `{"type":"resize","cols":N,"rows":M}`, translated to
  `pty.Setsize`.
- On WebSocket reconnect, the ring buffer is replayed before switching to live
  streaming, so a page refresh does not lose recent output.

### Grouped-session filtering

Because of the pane-PID collision described above, four call sites must exclude
`tsession-web-*` session names before they can be trusted for real-session matching:

- `internal/tmux/tmux.go:42` (`list-sessions`)
- `internal/tmux/tmux.go:167` (`ListPanes`)
- `internal/tmux/tmux.go:193` (`ListPanesWithTitle`)
- `internal/remote/gather.bash:152,169` (remote-side `list-sessions` / `list-panes`)

This filtering is required whether or not the web UI is running, since a stale
`tsession-web-*` session left behind after a crash must never be treated as a real
session's tmux target.

### Notifications

The web UI reuses `internal/notify`'s diff-based transition logic, but must not share
state with the desktop notifier. `~/.tsession/notify.json` is consumed and updated by
`watch --daemon --notify` and `browse --watch --notify` under `~/.tsession/notify.lock`;
if the browser observer wrote to the same file under the same lock, the two observers
would race to claim each transition and each would silently miss notifications the
other one already recorded.

`notify.Process(sessions)` is refactored into `notify.ProcessWithStore(sessions,
storePath)`, with `Process` delegating to it using the existing default path. The web
path calls `ProcessWithStore` with `~/.tsession/notify-web.json` and its own lock file,
so both observers can run concurrently and each fires exactly once per transition.

The browser requests `Notification.requestPermission()` on first load and renders the
same message text as the macOS path: `[name] done!` / `[name] needs your input`, where
`name` follows the same `Name → Summary → basename(cwd)` priority.

### Frontend

A single-page app: `index.html`, `app.js`, `app.css`, plus vendored `xterm.js`,
`xterm.css`, and the fit addon, all `go:embed`ed into the binary — no Node build step,
no CDN dependency, no framework. Layout is a CSS grid at a fixed 20/80 split (not
user-resizable in v1). Session rows reuse the existing state glyphs and source prefixes
so the visual vocabulary matches the terminal UI.

### Dependencies

Two new direct Go dependencies: `github.com/creack/pty` (PTY allocation) and
`github.com/coder/websocket` (WebSocket transport). Both are small, widely used, and
preserve the project's single self-contained binary.

## Error handling

- A non-loopback `--addr` causes `tsession serve` to exit immediately with an explicit
  error before binding.
- An attach failure closes the WebSocket with a reason string; the frontend shows an
  inline error banner with a Retry action.
- When the child process exits, `[process exited]` is written to the terminal and the
  ring buffer is retained, mirroring the `remain-on-exit` behavior of local bridges.
- Missing remote configuration or an unsupported transport type returns HTTP 400 with
  the explicit underlying message.
- A remote connection failure is surfaced as terminal output (visible to the user), not
  swallowed.
- A rename or alias persistence failure returns HTTP 500 with the underlying error, and
  the modal displays it rather than silently closing.

## Testing strategy

- **`attachcmd`**: local session with and without a tmux target; ssh, codespace, and
  devcontainer wrapping; quoting of session IDs, tmux targets, and host configuration;
  direct resume when remote tmux is unavailable.
- **`webterm`**: registry attach/reuse by key, subscriber fan-out, ring-buffer replay
  after reconnect, resize plumbing, and `Close` — all driven by a harmless fake command,
  not real tmux, so tests do not depend on a tmux binary being present.
- **`webui`**: `httptest`-based coverage of the `/api/sessions` JSON shape, the rename
  round-trip, refusal to bind a non-loopback address, and one SSE event frame.
- **`internal/tmux`**: a regression test proving that a `tsession-web-*` pane never
  overwrites a session's real `TmuxTarget` — the exact hazard the tmux probe exposed.
- **`notify`**: `ProcessWithStore` isolation, verifying two independent stores each
  observe and fire on the same transition without interfering with each other.

No browser-driven or end-to-end tests are in scope for v1.

## Relationship to existing code

`ensureRemoteBridge`, `EnsureBridge`, and the entire fzf-based `browse`/`popup` flow are
unmodified. The web UI is an additional, parallel front end built from the same session
model (`internal/sessions`, `internal/render`, `internal/config`, `internal/names`,
`internal/reponames`) — it introduces no new session-state semantics, sort order, or
classification logic.
