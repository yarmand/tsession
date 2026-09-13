# tsession native GUI

A [Wails v2](https://wails.io) desktop wrapper around the same web UI
server `tsession serve` runs (`internal/webui`). See
`../docs/superpowers/specs/2026-09-11-native-gui-design.md` for the design.

This is a separate Go module from the rest of the repo (own `go.mod`) so
Wails' CGO/native-webview dependencies never affect `go build` of the main
`tsession` CLI binary.

## Prerequisites

- Go 1.25+
- The Wails v2 CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Platform native webview runtime:
  - **macOS**: none — uses system WebKit.
  - **Windows**: the [WebView2 runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) (preinstalled on Windows 11).
  - **Linux**: `webkit2gtk` (e.g. `apt install libwebkit2gtk-4.1-dev` on Debian/Ubuntu).

No Node.js/npm is required — the embedded frontend
(`frontend/dist/index.html`) is a small hand-written loader page, not a
built JS app; `wails.json` sets `frontend:install`/`frontend:build` to
no-ops.

## Building

```bash
make gui
```

The resulting app bundle/binary is written to `gui/build/bin/` (for
example, `TSession.app` on macOS, `TSession.exe` on Windows, and
`TSession` on Linux). Install it wherever `tsession gui` looks for it:
next to the `tsession` binary, or the platform's conventional Applications
directory/search path (see `cmd/gui.go`'s `locateGUIApp` for the exact
search order). On Linux, `tsession-gui` is also accepted as a compatibility
symlink name, but the Wails output name is `TSession`.

`make gui` checks that the Wails CLI is installed before running
`wails build` inside this module. To build from inside `gui/` directly, run
`wails build`.

## Developing

```bash
cd gui
wails dev
```

Opens the native window with the embedded server running, same as a real
build, useful for quick iteration without a full `wails build`.
