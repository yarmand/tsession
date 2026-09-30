# Dedicated local terminal in serve and GUI

## Goal

Provide a persistent local shell inside `tsession serve` and the native GUI so
users can run commands such as `tsession new` without switching applications.
The shell starts in the user's home directory and survives browser tabs,
server shutdown, and GUI restarts.

## UI and behavior

Show a fixed **Local terminal** entry after the session list, including when
there are no agent sessions. Clicking or highlighting it and pressing Enter
opens its terminal in the existing terminal pane. `Alt+T` opens it directly
from either the session list or an active terminal, without requiring the
sidebar to be visible. Do not intercept this shortcut while an edit dialog is
open. The entry is not an agent session: it has no rename, repository alias,
code view, notifications, age, or agent state. Preserve normal session
selection, cursor movement, and per-session terminal switching.

## Server and lifecycle

Create a dedicated, deterministically named local tmux session lazily on first
attach, with its starting directory set to the user's home directory and its
default interactive shell. Reuse the session if it already exists; avoid
creating duplicates under concurrent first attaches. A failure to determine
the home directory or create/attach tmux must be returned to the browser and
shown to the user.

Expose a dedicated local-terminal WebSocket route rather than putting a fake
Copilot/pi session in the discovered session list. Use the existing `webterm`
PTY and grouped-tmux attachment behavior so browser terminal sizing does not
affect another tmux client. Its browser-side PTY/group may be torn down on
server shutdown, but **never kill the underlying dedicated tmux session**.
Reopening `serve` or GUI attaches to that same underlying session. The GUI
uses the existing embedded web server and requires no native-only terminal
implementation. Keep the route loopback-only under the existing server
listener and do not accept an arbitrary shell command, tmux target, or path
from the request.

## Verification

Test idempotent creation, error reporting, and lifecycle separation between
the temporary web attachment and persistent tmux shell. Test the browser
entry's fixed placement and keyboard activation, including an empty session
list and switching away and back. Check the existing terminal flow still
works for local and remote agent sessions, and bump the service-worker cache
version when changing static assets.
