# tsession native GUI (`tsession gui`) — design

## Problem

`tsession serve` (see `2026-09-11-web-session-ui-design.md`) already provides a browser-based
UI: a session list plus an xterm.js terminal, backed by a loopback-only HTTP server. Using it
today means running `tsession serve` and opening a browser tab manually — an extra step, and
one more browser tab among many, with no Dock icon, no app switcher entry, no independent
window.

Wrap the same server in a real native window: a proper installed application (`TSession.app`
on macOS, equivalent on Windows/Linux) that a user launches like any other app, with its own
icon, window, and lifecycle — while reusing 100% of the existing `internal/webui` server and
frontend assets unchanged.

## Approach

Use **Wails v2** to build a small native-window wrapper around the existing embedded HTTP
server. The wrapper owns only window chrome; all session/terminal logic stays exactly where it
is today in `internal/webui`.

Two separate artifacts come out of this:

1. **`gui/` — a new, separate Go module** (own `go.mod`, module path
   `github.com/yarma/tsession/gui`, with `replace github.com/yarma/tsession => ../`) containing
   the Wails app. Splitting it into its own module keeps Wails' CGO/native-webview dependencies
   completely out of the main `tsession` binary's build graph — `go build ./...` at the repo
   root stays CGO-free and cross-compilable exactly as it is today. The nested-module
   `replace` directive is what legally lets `gui/` import `internal/webui` despite Go's
   internal-package visibility rule (which is enforced by import-path prefix, not module
   boundary, so a nested module under the same repo still qualifies).
2. **A new `tsession gui` subcommand** in the existing CLI (`cmd/gui.go`) that is a pure
   **launcher** — it does not build or embed Wails itself. It locates the already-built native
   app on disk and starts it.

This keeps the existing single-binary `tsession` CLI exactly as it is (still `go build .`,
no Node/npm toolchain, no CGO) while giving GUI users a real installed application. The cost is
a second build pipeline (`wails build`, requiring Go + npm-equivalent toolchain + native
per-OS SDKs) that must run natively per target OS.

### Alternatives considered

- **Lightweight `webview_go` wrapper** (CGO binding directly to the OS webview, no Wails): less
  dependency weight, no separate build pipeline, could even live in the main binary behind a
  build tag. Rejected per explicit user preference for Wails, which trades that simplicity for
  a richer app framework (native menus/tray hooks available later, first-class packaging
  tooling, larger community/support surface).
- **Browser `--app=` kiosk mode** (no new framework, just launch installed Chrome/Edge in app
  mode): zero new dependencies, but no real installed app identity, depends on a specific
  browser being present, and no path to native menus/tray/packaging later. Rejected for the
  same reason.

## Architecture

### `gui/` module (Wails v2 app)

- `gui/go.mod`: separate module, `replace github.com/yarma/tsession => ../`, depends on
  `github.com/wailsapp/wails/v2` and the main module (for `internal/webui`).
- `gui/main.go`:
  - On startup, constructs and starts the same `internal/webui.Server` used by
    `tsession serve` — identical session registry, PTY attach (`internal/webterm`,
    `internal/attachcmd`), SSE notifications, and WebSocket terminal — bound to
    `127.0.0.1:<port>`. Port selection: try the same default port `tsession serve` uses; if
    taken, fall back to an OS-assigned ephemeral port (`:0`) so a second launch (or a
    concurrently running `tsession serve`) never fails to start.
  - Opens a Wails window pointing at that local server's URL.
  - No Wails Go↔JS runtime bindings are used. Wails' own frontend-asset pipeline (which expects
    an npm-built `frontend/dist`) is sidestepped: the app's embedded "frontend" is a single
    tiny static `index.html` loader (a few lines) that immediately does
    `location.replace("http://127.0.0.1:<port>/")` once the embedded server signals it's ready.
    This means the real UI stays exactly `internal/webui/static` served over plain HTTP,
    completely unchanged — no npm/Vite build step is needed anywhere in this feature.
  - Startup ordering: the embedded server starts first and signals readiness (its listener is
    bound and `Serve` has been called) before the Wails window's loader page attempts to load
    the URL; the loader retries with backoff for a bounded time if the very first load races
    ahead of the listener.
- App icon: reuse the existing PWA icon set already produced for the web UI
  (`internal/webui/static/icons/icon-512.png` et al. as the source art) rather than
  regenerating new artwork — Wails' `wails build` config points at a copy of that same PNG.

### `cmd/gui.go` (launcher, in the existing `tsession` binary)

- New subcommand `tsession gui`. Responsibilities: locate the platform-appropriate installed
  native app bundle, then launch it and exit (does not itself host any server or window).
- Search order, preferring the location most likely to be the version the user actually
  intends to run:
  1. Next to the currently running `tsession` executable (covers side-by-side dev/test installs
     and simple manual deployments).
  2. Platform-conventional install locations:
     - macOS: `/Applications/TSession.app`, `~/Applications/TSession.app`
     - Windows: a conventional install dir (e.g. `%LOCALAPPDATA%\TSession\TSession.exe`)
     - Linux: `tsession-gui` on `PATH`
- Launch mechanism: macOS → `open -a <path to .app>`; Windows/Linux → exec the located binary
  directly, detached from the CLI's own process group so closing the terminal doesn't kill the
  GUI.
- Not found → exit non-zero with an explicit error naming every path searched and pointing at
  build/install instructions (e.g. "run `wails build` in `gui/`, or install TSession.app").
  The launcher never attempts to build the app itself — that requires the Wails CLI and a
  frontend-adjacent toolchain, well outside a simple CLI launcher's job.

### Cross-platform runtime requirements

- **macOS**: uses system WebKit; no extra runtime to install.
- **Windows**: requires the Microsoft WebView2 runtime. Preinstalled on Windows 11; Wails'
  installer/build tooling can bootstrap/check for it on Windows 10. Documented as a runtime
  prerequisite, not silently handled.
- **Linux**: requires `webkit2gtk` as a system package (standard Wails Linux prerequisite,
  documented, not auto-installed).
- `wails build` must be run natively on each target OS to produce that OS's artifact — there is
  no cross-compilation of the actual app bundle. A CI matrix (macOS/Windows/Linux runners) to
  automate producing all three release artifacts is explicitly out of scope for this design
  (see below) and left as future work.

## Error handling

- Embedded server fails to bind (both the default port and the `:0` fallback fail, e.g. no
  loopback interface available): show a native error dialog via Wails' dialog API, then exit —
  never show a blank/white window.
- Loader page's initial load races the server's readiness: bounded retry with backoff (e.g. a
  few attempts over ~2s) before surfacing a "failed to start" state in the window itself.
- Launcher (`tsession gui`) finds no installed app: explicit error listing every path checked,
  non-zero exit, and instructions for building/installing it. No silent no-op.
- Launcher finds an app but the OS launch call itself fails (e.g. permissions, corrupt bundle):
  propagate that OS-level error message directly rather than masking it.

## Testing

- `cmd/gui_test.go`: table-driven tests for the app-location search logic (existing-adjacent
  path found / platform-path found / nothing found → correct error listing all searched paths),
  built against temp directories — mirrors the style of existing `cmd/*_test.go` files. The
  actual OS launch call (`open -a`, `exec`) is not exercised in tests, only the path-resolution
  logic, matching how e.g. `remote_bridge_test.go` isolates command construction from actual
  execution.
- `gui/` module: kept intentionally small; no meaningful automated test surface beyond what
  could be manually factored out (e.g. port-fallback selection logic, if written as a pure
  function). Verification of the actual native window is manual, per platform, before each
  release — this mirrors the "no browser or end-to-end tests" stance already taken for the web
  UI itself.

## Out of v1 scope

- CI/release pipeline for building and publishing the three platform artifacts.
- Code signing and macOS notarization (an unsigned `.app` will trigger Gatekeeper warnings on
  first launch; acceptable for v1, revisit before wider distribution).
- Auto-update mechanism.
- Windows/Linux installer polish (e.g. an actual `.msi`/`.deb`/AppImage) — v1 ships the raw
  `wails build` output.
- Native menu bar, system tray icon, or any other Wails-native-chrome feature beyond a plain
  window — the window is intentionally "just the same browser UI in a native frame" for v1.
- Wails Go↔JS runtime bindings — deliberately unused; all logic stays in `internal/webui`
  over plain HTTP exactly as `tsession serve` already works.
