# Remote Session Local Bridge Design

**Date:** 2026-07-15
**Status:** Approved for planning

## Goal

When a remote session is selected from `tsession browse`, display that session
on the selected local tmux client without replacing or modifying an existing
local project pane.

The selected client switches to a persistent local bridge session. The bridge
owns the interactive SSH, Codespaces, or devcontainer connection and reconnects
to the same remote Copilot session when selected again.

## Current State

- Remote sessions are collected by an installed remote `tsession` binary and
  displayed in local `list` and `browse` output.
- `browse --target` resolves a local tmux client once and passes that client to
  `resume`.
- Local sessions switch the selected client directly to their local tmux target.
- Remote resume currently either finds an already-open local pane connected to
  the remote or starts an interactive transport command in the picker process.
- Remote snapshots do not expose the remote tmux target or whether tmux is
  available on the remote host.

The direct interactive fallback does not fit target-client behavior: fzf uses
`execute-silent`, the connection is not represented by a reusable local tmux
target, and the selected client has nothing persistent to switch to.

## Decisions

1. Fix `--target pick` before implementing remote bridges.
2. Use one deterministic local bridge tmux session per remote session.
3. Reuse the same bridge from any local target client.
4. Keep the picker in its own `session-nav` tmux session.
5. Switch only the selected local tmux client to the bridge.
6. Prefer attaching to the exact existing remote tmux pane.
7. If remote tmux exists but the session has no target, create or reuse a
   deterministic remote tmux session running `copilot --resume=<id>`.
8. If remote tmux is unavailable, run `copilot --resume=<id>` directly.
9. Enable local tmux `remain-on-exit` for the bridge pane.
10. After disconnection, retain the dead bridge pane as a reconnect screen.
11. Selecting the session again respawns a dead bridge connection.
12. Leaving a bridge never terminates a remote tmux session.

## Architecture

### Prerequisite: restore target-client selection

The current non-watch browse path resumes a selection twice:

1. The fzf `enter` binding executes `tsession resume --target=<resolved-tty>`.
2. After fzf accepts, `Browse` calls `Resume([]string{id})` without the target.

The second call switches the navigation client and makes `--target` appear
ineffective. Fix this before adding bridge behavior:

- Inside tmux, the fzf `enter` binding remains the single owner of resume and
  target switching.
- After fzf returns in non-watch mode, `Browse` must not resume the accepted ID
  again.
- Outside tmux behavior may continue returning an accepted ID for its caller,
  but must have one explicit resume owner.

Explicit `--target pick` must always show a client picker, even when only one
eligible non-navigation client exists. The picker excludes:

- the client currently displaying `session-nav`;
- clients without a usable TTY.

The selected TTY is resolved once when browse starts and is passed unchanged to
every resume action during that browse process. Direct `--target=/dev/...`
continues to bypass the picker.

Re-entering `tsession browse` from outside tmux must apply the new invocation's
arguments even when `session-nav` already exists. The launcher must restart the
navigator pane with the new browse command before attaching; `tmux new-session
-A` must not silently reuse an older browse process with stale flags.

### Remote snapshot metadata

Extend the snapshot protocol with host and session attachment metadata:

```go
type SnapshotPayload struct {
    TmuxAvailable bool             `json:"tmuxAvailable"`
    Sessions      []SessionPayload `json:"sessions"`
}

type SessionPayload struct {
    // Existing fields...
    TmuxTarget string `json:"tmuxTarget,omitempty"`
}
```

The remote snapshot builder already loads tmux sessions, panes, and process
metadata when tmux is available. After merging local remote-host sessions, it
includes each resolved target, such as `famstack:2.0`, in the payload.

Failure to execute `tmux list-*` is interpreted as `TmuxAvailable: false` only
when tmux is absent. Other snapshot failures remain explicit errors.

The local `sessions.Session` model adds distinct remote attachment fields:

```go
RemoteTmuxTarget    string
RemoteTmuxAvailable bool
```

Existing `TmuxName` and `TmuxTarget` continue to mean local tmux locations.
`RemoteTmuxTarget` is consumed only by the bridge command builder.

### Snapshot execution with and without remote tmux

Remote tmux is optional:

- When tmux is available, the local manager may keep using the managed
  `tsessiond` tmux session before requesting a one-shot snapshot.
- When tmux is unavailable, the local manager skips daemon creation and invokes
  `<remote-binary> remote rpc snapshot` directly through the configured
  transport.

Snapshot collection must therefore probe tmux capability before attempting
daemon startup. Absence of tmux is a supported capability result, not a remote
warning. The remote binary remains required in both modes.

### Local bridge identity

The bridge session name is deterministic:

```text
tsession-r-<sanitized-origin>-<short-session-id>
```

The implementation must include enough normalized session-ID characters to
avoid practical collisions and verify an existing bridge's stored identity
before reuse. Store the full origin and session ID as tmux session or pane user
options:

```text
@tsession-origin
@tsession-session-id
```

If a generated name belongs to different metadata, add a stable suffix rather
than reusing the wrong bridge.

### Bridge command

The bridge pane runs one interactive transport command generated from the
configured remote:

- SSH: `ssh -t <host> <remote-command>`
- Codespaces: `gh codespace ssh ... -t -- <remote-command>`
- Devcontainer: `docker exec -it ... <remote-command>`

The remote command is selected in this order:

1. **Existing remote target**
   - Run `tmux attach-session -t <target>`.
2. **Tmux available, no target**
   - Create or reuse a deterministic remote tmux session for the Copilot
     session.
   - Start `copilot --resume=<id>` in that remote tmux session.
   - Attach interactively.
3. **Tmux unavailable**
   - Run `copilot --resume=<id>` directly.

All shell fragments and tmux target values must use existing quoting helpers or
new focused quoting helpers with unit tests.

### Bridge lifecycle

On remote selection:

1. Resolve the configured remote and cached session metadata.
2. Resolve the selected local tmux client as today.
3. Derive and validate the bridge session identity.
4. If the bridge does not exist:
   - create it detached;
   - enable `remain-on-exit` on its pane;
   - start the interactive bridge command.
5. If the bridge exists and its pane is alive, reuse it without starting a
   second connection.
6. If the bridge exists and its pane is dead, respawn the pane with the current
   bridge command.
7. Switch only the selected local client to the bridge target.

The dead pane displays the transport's final output. Its pane title or an
explicit final line should tell the user to select the session again to
reconnect.

## Selection Data Flow

The fzf row continues to pass session ID, origin, and summary to `resume`.
`resume` loads the cache/live session model to obtain:

- source type;
- remote tmux target;
- remote tmux availability;
- configured transport.

The fzf row does not need to embed transport commands or remote tmux metadata.
This keeps command construction in Go rather than in fzf field interpolation.

For a remote selection, bridge resolution becomes the default. Existing local
pane matching remains a fast path only when it identifies the deterministic
bridge for the same remote session. Arbitrary pane-title matches must not switch
to an unrelated local or remote connection.

## Target Client Behavior

`--target` keeps its current semantics:

- empty: switch the current tmux client;
- `/dev/...`: switch that client;
- any other value: select a client interactively once when browse starts.

Bridge creation is independent of the selected client. Multiple clients can
switch to the same bridge, and selecting the same remote session from another
client does not create another SSH connection.

## Error Handling

- Missing remote configuration: return an explicit error.
- Unsupported transport: return an explicit error.
- Local bridge creation/respawn failure: return an explicit error.
- Target-client switch failure: leave the bridge running and return the error.
- Remote connection failure: retain the bridge pane and its output.
- Remote tmux absent: use direct `copilot --resume`; do not warn.
- Remote tmux target stale: the attach command should fall back to the
  deterministic create/reuse path in the same remote shell command.
- Missing remote `copilot`: show its command error in the bridge pane.

`browse --watch` continues after a successful switch. A bridge setup error must
be visible rather than swallowed by fzf `execute-silent`. Failures that happen
before bridge creation are displayed using `tmux display-message`; failures
inside the transport remain visible in the retained bridge pane.

## Testing Strategy

### Protocol and session conversion

- Snapshot reports tmux availability.
- Existing remote pane target is serialized and restored locally.
- No-tmux hosts return sessions with direct-resume capability.
- No-tmux hosts skip managed-daemon startup and use one-shot snapshot RPC.

### Command construction

- SSH attach to existing target.
- Codespaces attach to existing target.
- Devcontainer attach to existing target.
- Tmux create/reuse fallback when no target exists.
- Direct Copilot resume when tmux is unavailable.
- Shell quoting for session IDs, targets, host configuration, and paths.

### Bridge lifecycle

- Deterministic bridge naming and collision handling.
- New bridge creation with `remain-on-exit`.
- Live bridge reuse without respawn.
- Dead bridge respawn.
- Full origin/session identity validation.
- Selected client switch uses `tmux switch-client -c <tty>`.
- Failure to switch clients does not destroy the bridge.

### Resume integration

- Explicit `--target pick` always opens the client picker.
- The navigation client is excluded from target candidates.
- Non-watch selection performs exactly one resume with the resolved target.
- Watch selection reuses the same resolved target across selections.
- Remote fzf selection creates a bridge and switches the requested client.
- Selecting the same remote session twice reuses one bridge.
- A disconnected bridge reconnects on the next selection.
- Local session resume behavior remains unchanged.

## Non-Goals

- Rendering a remote terminal inside the fzf preview.
- Multiplexing several remote sessions through one local bridge.
- Automatically destroying idle bridge sessions.
- Synchronizing terminal scrollback between local clients.
- Replacing SSH/Codespaces/devcontainer transports.
- Requiring tmux inside devcontainers or other remote environments.
