# Remote Server Watch Design

**Date:** 2026-07-13  
**Status:** Approved for planning

## Goal

Keep the existing remote configuration model, but replace command-by-command remote assumptions with a managed remote `tsession` server model.

The local `tsession` process remains the orchestrator. Remote machines run a `tsession` daemon that watches local instances and returns state snapshots to the local machine during watch/list/browse refresh cycles.

## Scope

This spec covers, in one cohesive design:

1. Remote server architecture and watch data path.
2. Runtime detection + remote binary install/update logic.
3. GitHub Actions release pipeline for cross-compiled binaries.

## Decisions

1. **Transport:** SSH-managed execution + JSON RPC over stdio.
2. **Server lifecycle:** Long-running daemon per remote host.
3. **Daemon host process manager:** Remote tmux session (`tsessiond`).
4. **Snapshot mode:** Full snapshot responses (no delta protocol in v1).
5. **Remote session filtering:** Remote daemon returns **active sessions only**.
6. **Active definition:** `state != exited && state != unknown && state != inactive-idle`.
7. **Runtime support (v1):** `linux/amd64`, `linux/arm64`, `darwin/arm64`.
8. **Version selection policy:** Prefer client tag match; fallback to latest release if no exact tag for runtime.
9. **Version/runtime refresh frequency:** At most once every 24h per remote, unless forced by `--update-remote`.
10. **Forced update behavior:** `--update-remote` applies to all configured remotes.
11. **Config extension:** Add per-remote `active` flag, default `true`.

## Architecture

### Local side

Add a remote manager flow used by `watch`, `list`, and `browse`:

1. Load configured remotes and keep only `active: true`.
2. For each remote, ensure availability of an installed runtime-compatible binary (respecting 24h refresh cache unless forced).
3. Ensure remote daemon is running in tmux.
4. Request `snapshot` over SSH stdio RPC.
5. Merge returned sessions into existing list rendering grouped by remote origin.

### Remote side

Add `tsession remote serve` command:

1. Runs continuously in remote tmux.
2. Responds to RPC requests (`health`, `snapshot`).
3. Reads local remote host sources (`~/.copilot`, pi state, tmux/process metadata as needed).
4. Applies active-session filter server-side.
5. Returns full snapshot payload and metadata (`serverVersion`, `runtime`, `generatedAt`).

## RPC Protocol (v1)

### Transport

- Request/response over SSH stdio.
- JSON lines framing (one JSON object per line).

### Requests

- `health`
- `snapshot`

### Responses

Common envelope:

```json
{
  "protocolVersion": 1,
  "ok": true,
  "error": "",
  "serverVersion": "vX.Y.Z",
  "runtime": "linux-amd64",
  "generatedAt": "2026-07-13T15:00:00Z",
  "payload": {}
}
```

`snapshot` payload contains active sessions only.

### Compatibility

- Unknown protocol or incompatible version returns typed error with actionable message (upgrade local and/or remote binary).

## Remote Binary Resolution and Install

### Resolution

For each remote, local manager determines desired binary version:

1. Try exact match with local client tag.
2. If no matching release asset exists for the remote runtime, use latest release asset for that runtime.

### Runtime detection

Remote probe maps `uname -s` + `uname -m` to supported assets:

- Linux x86_64 -> `linux-amd64`
- Linux aarch64/arm64 -> `linux-arm64`
- Darwin arm64 -> `darwin-arm64`

Unsupported runtime returns explicit warning and skips that remote.

### Refresh policy

- Do **not** resolve version/runtime every watch tick.
- Re-check at most once per 24h per remote.
- `--update-remote` forces immediate re-check/install for all configured remotes.

### Install location and state

- Remote binary path: `~/.tsession/bin/tsession`
- Per-remote metadata/manifests: `~/.tsession/remotes/<name>/...` (installed version, last checked timestamp, runtime)

## Remote Daemon Lifecycle

Daemon is managed in remote tmux, e.g.:

```bash
tmux new-session -Ad -s tsessiond '<remote-binary> remote serve'
```

Behavior:

- Reuse existing `tsessiond` session when healthy.
- Restart if absent or unhealthy.
- Snapshot calls are non-blocking across remotes (one remote failure does not block others).

## Config Changes

Existing config stays valid. Add optional `active`:

```yaml
remotes:
  - name: devbox
    host: devbox
    active: true
```

Rules:

- Omitted `active` -> treated as `true`.
- `active: false` keeps remote configured but skips all remote operations for it.

## CLI Changes

Add `--update-remote` to remote-aware commands:

- `tsession watch --update-remote`
- `tsession list --update-remote`
- `tsession browse --update-remote`

Semantics:

- Force version/runtime refresh for all configured remotes before normal command behavior.
- Without flag, use 24h refresh TTL.

## GitHub Release Pipeline

Create GitHub Actions workflow triggered on version tags (`v*`):

1. Build cross-compiled binaries for:
   - `linux/amd64`
   - `linux/arm64`
   - `darwin/arm64`
2. Produce deterministic asset names (include version, os, arch).
3. Publish assets to GitHub Releases for the tag.
4. Ensure assets can be discovered by resolver logic.

## Error Handling

Per-remote failures produce warnings and continue:

- SSH connect failure
- Unsupported runtime
- Missing/failed release download
- Daemon startup failure
- RPC timeout or protocol mismatch

No silent fallback to stale/unknown state without explicit warning context.

## Testing Strategy

1. **Config tests**
   - `active` default true
   - `active: false` exclusion behavior
2. **Resolver tests**
   - exact-tag selection
   - fallback to latest
   - unsupported runtime behavior
   - 24h TTL vs forced update
3. **Runtime mapping tests**
   - Linux amd64/arm64, Darwin arm64 mappings
4. **Remote manager tests**
   - install/ensure-daemon/snapshot happy path
   - per-remote failure isolation
5. **RPC tests**
   - request/response schema
   - protocol mismatch handling
6. **CI workflow tests**
   - matrix outputs
   - release artifact naming and presence

## Non-Goals

- Delta snapshot protocol in v1.
- Windows runtime support in v1.
- Replacing SSH transport in v1.
