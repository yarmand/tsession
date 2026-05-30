# Pi + tsession Integration Design

## Goal

Allow tsession to discover and display pi sessions alongside Copilot CLI sessions, with the same state tracking (working, question, done, idle, exited) and tmux pane correlation.

## Architecture

Two components:

1. **Pi extension** (`~/.pi/agent/extensions/tsession-state.ts`) — writes per-session state files
2. **tsession changes** — reads pi state files and merges into the existing session list

## Pi Extension: State Writer

### State File Location

```
~/.tsession/pi-state/<session-id>.json
```

### State File Schema

```json
{
  "id": "019e771a-b798-7cbc-a430-c40ff48215c4",
  "state": "done",
  "cwd": "/Users/yarma/src/tsession.worktrees/pi",
  "summary": "first user message or session name",
  "updatedAt": "2026-05-30T04:31:09Z",
  "pid": 12345,
  "sessionFile": "~/.pi/agent/sessions/--Users-yarma-src-tsession.worktrees-pi--/2026-05-30T04-18-20-184Z_019e771a-b798-7cbc-a430-c40ff48215c4.jsonl"
}
```

### Event → State Mapping

| Pi Event | State Written |
|----------|---------------|
| `session_start` | `idle` |
| `turn_start` | `working` |
| `agent_end` | `done` or `question` |
| `session_shutdown` | `exited` |

### Question Detection

After `agent_end`, read session entries via `ctx.sessionManager.getEntries()`, find the last assistant message, extract trailing text content, check if it ends with `?` (after trimming whitespace).

### File Lifecycle

- Created on `session_start`
- Updated atomically (write-to-temp + rename) on every state transition
- Marked `exited` on `session_shutdown`
- Stale cleanup: on `session_start`, remove exited state files older than 1 hour

## tsession Changes: Pi Session Discovery

### New Package: `internal/pisessions`

Scans `~/.tsession/pi-state/*.json`, reads each file, returns a slice of `sessions.Session`.

### State Mapping

| Pi State String | tsession State |
|-----------------|----------------|
| `"working"` | `StateWorking` |
| `"question"` | `StateWaiting` |
| `"done"` | `StateDone` |
| `"idle"` | `StateActiveIdle` |
| `"exited"` | `StateExited` |

### Session Struct Changes

Add `Source string` field to `sessions.Session` (values: `"copilot"`, `"pi"`) for rendering differentiation.

### Integration Point

`loadAllLive()` in `cmd/watch.go` calls `pisessions.LoadAll()` and appends results to the merged Copilot session list. Existing sort/filter/render pipeline handles them uniformly.

### PID-based Tmux Matching

Pi state file includes `pid`. Feed through existing `ResolveTmuxByPID` — same ancestor-chain walk.

### Done-State Clearing

Existing `donestate` package tracks session IDs. On pane switch, clears done → active. Works for pi sessions identically (same ID-based mechanism).

## Resume Behavior

| Condition | Action |
|-----------|--------|
| Pi session has tmux match | Switch to that pane |
| Pi session has no tmux match | Run `pi --session <session-id>` in a new pane |

## Edge Cases

| Case | Behavior |
|------|----------|
| Pi crashes (no `session_shutdown`) | tsession checks if PID alive; if dead → treat as `exited` |
| Multiple pi sessions in same cwd | Each has own state file keyed by session ID |
| State file write races | Atomic write (temp + rename) |
| Old exited files accumulating | Extension cleans up >1h old on `session_start` |
| Stale PID detection | `kill(pid, 0)` check when `state != "exited"` |
