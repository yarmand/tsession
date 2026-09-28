# VS Code web pane in `serve` / `gui`

## Problem

`tsession serve` (and the Wails `gui`, which embeds the identical
`internal/webui` server) shows a session list plus one browser terminal. There
is no way to edit the session's code without leaving the app.

This adds an optional **code view** docked to the right of the terminal:

- Off by default.
- Toggled with **Alt+E** while the **session list panel is focused**.
- Backed by a `code serve-web` instance scoped to the session's `CWD`.
- Bound to a specific session: switching sessions hides it; switching back
  shows it again **without relaunching** VS Code.
- Works for remote sessions by tunnelling to the remote `code serve-web`.

## Architecture

Mirrors the existing terminal architecture exactly, one layer per concern:

| Existing | New | Responsibility |
|---|---|---|
| `internal/webterm` | `internal/codeserver` | Registry of long-lived `code serve-web` children keyed by `(origin, id)`; owns launch, port discovery, log buffer, dialer, teardown. |
| `internal/attachcmd` | `internal/codecmd` | Pure command/script building: local vs ssh / codespace / devcontainer, `code` binary resolution. Fully unit-testable, no process I/O. |
| `webui.handleTerminal` | `webui` code routes | HTTP surface only; everything injected via `WithCodeServer(...)`. |

### Reaching the VS Code UI: same-origin reverse proxy

`code serve-web` supports `--server-base-path`, so it emits **absolute URLs
already prefixed** with our path. The server therefore:

1. Computes a stable key `k = sha256(origin \x00 id)[:6]` hex-encoded (same
   scheme as `attachcmd.WebSessionName`), via `codecmd.Key`.
2. Launches with `--server-base-path /api/code/<k>`.
3. Reverse-proxies `/api/code/<k>/...` → the instance, **without rewriting
   the path**, using stdlib `net/http/httputil.ReverseProxy`. Its
   `Transport.DialContext` always dials the target `codeserver.Instance` via
   `Instance.Dial()` regardless of the requested address — the same proxy
   code serves a local loopback dial, an established ssh/codespace tunnel's
   local end, or a fresh per-connection `docker exec` stdio relay uniformly.
   `ReverseProxy` also handles the 101 protocol-switch upgrades VS Code web
   needs, with no new dependency.

This keeps the iframe same-origin: no cross-origin storage partitioning, no
`X-Frame-Options`/`frame-ancestors` exposure, and identical handling for
local and remote sessions.

### Launch command

```
<code> serve-web --host 127.0.0.1 --port 0 \
  --without-connection-token --accept-server-license-terms \
  --server-base-path /api/code/<k> \
  --default-folder <session cwd> \
  --server-data-dir <per-key dir>
```

`--host 127.0.0.1` keeps it loopback-only on both ends, matching `serve`'s
existing "PTYs must never be reachable off-host" rule; `--without-connection-token`
is safe *because* of that binding plus the proxy being the only reachable
path.

The actual port is discovered by scanning the child's combined stdout/stderr
for the `Web UI available at http://…:<port>` line (ANSI-stripped, since
remote transports run under a PTY). Startup can take minutes on first run
(VS Code downloads the server), so start is asynchronous and the frontend
polls status, showing a "Starting VS Code…" placeholder with a captured log
tail.

Confirmed against the real `code` CLI (v1.138.0): the startup line format
is exactly `Web UI available at http://127.0.0.1:<port>/api/code/<key>`
(no trailing slash); the first request during an instance's first-ever
launch returns `202` with an auto-reloading "downloading" page, then `200`
once ready.

### Transports

| Origin type | Launch | Dial from proxy |
|---|---|---|
| local | direct child process | `net.Dial("tcp", "127.0.0.1:<port>")` |
| `ssh` | `r.ResumeCommand()` + login-shell script (reuses `shellutil`) | one persistent `ssh -N -L 127.0.0.1:<L>:127.0.0.1:<R> host` process; dial `<L>` |
| `codespace` | `gh codespace ssh -t --` + same script | `gh codespace ports forward <R>:<L>`; dial `<L>` |
| `devcontainer` | `docker exec -it` + same script | per-connection `docker exec -i … sh -c` stdio relay (`nc` → `socat` → `python3` ladder), adapted to `net.Conn` via `stdioConn` |

`-t` (already in `ResumeCommand`) gives the remote process a PTY so it
receives SIGHUP when tsession kills the launcher — no orphaned remote VS
Code servers.

### `code` binary configuration

`internal/config` supports `code_command` as a top-level scalar and as a
per-remote override:

```yaml
code_command: /usr/local/bin/code      # top-level default
remotes:
  - name: box
    code_command: /home/me/.local/bin/code
```

`config.Remote.CodeBinary(cfg)` resolves: the remote's own `CodeCommand` if
set, else the top-level default, else empty (meaning: resolve `code` from
PATH). Unset resolves locally via `exec.LookPath` and remotely via
`shellutil.CodeResolverCommand()`, so the user's login-shell PATH applies —
mirroring `CopilotResolverCommand`.

### HTTP surface (`internal/webui`)

| Route | Purpose |
|---|---|
| `POST /api/codeserver/{origin}/{id}` | Start (or return existing, idempotent while starting/running). Returns `{key, path, status}` immediately. |
| `GET /api/codeserver/{origin}/{id}` | `{status: "starting"\|"running"\|"failed"\|"stopped", path, error, log}` |
| `DELETE /api/codeserver/{origin}/{id}` | Explicit stop + teardown. |
| `/api/code/{key}/...` | Reverse proxy to the instance (all methods, incl. WS upgrade). |

Distinct `/api/codeserver` vs `/api/code` prefixes avoid any route
ambiguity. Without `WithCodeServer(...)` these respond `501`, matching how
`handleTerminal` degrades today.

### Frontend (`internal/webui/static`)

- `#terminal-pane` is a flex row: `#terminal-wrap` | `#code-resize` |
  `#code-pane`. The divider reuses the `#sessions-resize` pointer-capture
  pattern; width persisted in `localStorage` (`tsession-code-width`).
- `#code-pane` header: session label, status text, and a **✕** button that
  issues `DELETE` (the only thing that stops VS Code).
- **One `<iframe>` per session key, retained in the DOM** and shown/hidden
  via CSS. This is what makes "switch back" instant and preserves editor
  state — a single shared iframe whose `src` is rewritten would reload VS
  Code on every switch. State lives in `app.js`'s `state.codeViews` map:
  `Map<sessionKey, {s, path, status, error, log, iframe, visible, pollTimer}>`.
- `Alt+E` (guarded on `ev.code === "KeyE"`, no other modifiers, and
  `state.focusTarget === "list"`, consistent with the existing `Alt+H` /
  `Alt+/` handlers) acts on the **highlighted** row: selects that session if
  it isn't already selected, then toggles its code view's `visible` flag.
- Toggling off only hides the pane (the server keeps running), per the
  chosen semantics; only the ✕ button stops it.
- `selectSession` calls `renderCodePane()`, which hides every code pane then
  re-shows the newly selected session's iframe if it has one and is marked
  visible — this is what satisfies "reactivating a session that already has
  a code view: do not relaunch."
- Terminal re-fits after any width change via the existing `ResizeObserver`
  on `#terminal`.
- Hint line and tooltips mention Alt+E.

### Lifecycle

Teardown happens on **explicit ✕** and on **registry shutdown** only —
identical to `webterm`. `cmd/serve.go`'s shutdown path and `gui/app.go` both
call `codeRegistry.Shutdown()` alongside the existing `registry.Shutdown()`.
A disconnected browser tab never stops VS Code.

## Notes & risks

- **First launch is slow.** `code serve-web` downloads the server build on
  first use, confirmed to take ~20s+ even on a fast local machine; the UI
  shows a placeholder with a log tail rather than a hard timeout.
- **`--server-base-path` behaviour** was the main technical assumption
  going in. It is supported and was validated twice: via an `httptest`-based
  fake `code` script asserting unmodified path pass-through, and manually
  against the real `code` CLI end-to-end.
- **Devcontainer dialing** is the weakest transport: `docker exec` has no
  port forwarding, so it depends on `nc`/`socat`/`python3` existing in the
  image. Failure surfaces as a captured stderr message in the code pane's
  error state rather than a blank iframe.
- **Security posture is unchanged**: everything stays behind the existing
  loopback-only listener; VS Code binds `127.0.0.1` on its own host and is
  reachable only through the proxy.
- No new Go module dependencies; no frontend build step (consistent with
  the existing no-Node-toolchain policy).
- `tsession vscode <id>` (desktop VS Code) is untouched and remains
  separate from this in-browser code view.

## Packages and files

- `internal/config` — `Config.CodeCommand`, `Remote.CodeCommand`,
  `Remote.CodeBinary(cfg)`; parser support for top-level scalars.
- `internal/shellutil` — `CodeResolverCommand()`.
- `internal/codecmd` — `Key(origin, id)`, `Build(...)`, `TunnelCommand(...)`.
- `internal/codeserver` — `Registry`, `Instance`, `LocalDialer`,
  `RemoteTunnelDialer`, `DevcontainerDialer`, `PortReadyFor`.
- `internal/webui/code.go` — `WithCodeServer`, `CodeConfigProvider`,
  `handleCodeServerStart/Status/Stop`, `handleCodeProxy`.
- `internal/webui/static/{index.html,app.css,app.js}` — code-pane layout and
  Alt+E behaviour.
- `cmd/serve.go`, `gui/app.go` — wiring and shutdown.

## Debug logging for user interactions and failures

Every meaningful user interaction, and every failure regardless of which
side detects it, is logged in the `serve` process log:

- `POST /api/debug` (`internal/webui/debug.go`) accepts `{action, sessionId,
  origin, sessionName, detail, level}` from the browser and logs it via
  `Server.logInteraction` as `serve user interaction: action=... session=...
  origin=... level=... name="..." detail="..."`. `level` defaults to
  `"info"`; the frontend's `reportFailure` helper sets `"error"`.
- The frontend (`app.js`) calls `reportUserInteraction` on session selection,
  focus changes, sidebar/code-pane show/hide/resize, and code-view
  show/hide/close, and calls `reportFailure` when it observes a code view
  failing to start or poll, or a terminal WebSocket erroring out.
- The server also logs failures it detects on its own, independent of the
  browser: every error branch in `handleCodeServerStart/Status/Stop`, and —
  critically for "code is failing to start" — `writeCodeServerResponse`
  logs `code-view-instance-failed` (with the captured log tail, not just the
  exit error) whenever an instance has transitioned to `codeserver.
  StatusFailed`, whether that's discovered synchronously on start or later
  while the browser polls status.
- `grep 'level=error'` in the `serve` log finds every failure from either
  side without needing to correlate browser and server logs separately.
