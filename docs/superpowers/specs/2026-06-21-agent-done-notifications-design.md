# Agent done / asking-question notifications — Design

## Problem

When a Copilot or pi agent finishes a turn (transitions to `done`) or stops to
ask the user something (transitions to `question`), there is no signal outside
the session's own pane. A user juggling several sessions has to poll
`tsession list`/`browse` to notice. We want an OS notification (with sound) the
moment an agent becomes done or starts waiting for input.

On macOS the notification is delivered via:

```
osascript -e 'display notification "[<agent name>] done!" sound name "Tink"'
```

## Goals

- Fire an OS notification when a tracked session **enters** the `done` or
  `question` state.
- Cover **all** tracked sessions, local and remote.
- Opt-in via a `--notify` flag (off by default) on the commands that observe
  state over time: `watch`, `list`, and `browse`.
- Fire each transition **exactly once**, even when multiple `--notify`
  observers run concurrently.
- Do not flood the user with notifications for sessions that were already
  `done`/`question` when observation started.

## Non-goals (YAGNI)

- Linux / `notify-send` support (macOS only for now; other platforms are a
  silent no-op).
- Configurable sounds or message text.
- Notifications for the `exited` state or any state other than `done`/`question`.

## Background: how state and refresh work today

- `internal/sessions` derives a `State` per session
  (`working`, `question` (=`StateWaiting`), `done` (=`StateDone`), `active`,
  `idle`, `exited`). `done` is a synthetic state computed in
  `sessions.Merge` from a `working → idle` transition tracked in the
  `internal/donestate` runtime file. `question` is derived directly from the
  event log in `classifyFromEvents`.
- `donestate` transition detection is **consume-once**: every live load
  (`list`, `browse`, `popup`, `watch`) runs `Merge`, which advances and persists
  `LastState`. Whichever process runs first consumes the transition. Therefore
  notifications **cannot** piggy-back on `donestate` — a non-notifying `list`
  call would swallow the transition before a notifier saw it.
- `watch --daemon` is a long-lived loop calling `refresh` every interval; it
  builds the full merged session list (local + remote) directly.
- `browse --watch` does **not** loop in Go. It launches `fzf` with a
  `--reload` binding that re-runs `tsession list --fzf` as a fresh subprocess
  every 5s. So "browse auto-refresh" is really repeated `tsession list`
  invocations. This is why the notifier state must be **persisted to disk**, not
  held in memory.

## Design

### New package: `internal/notify`

```
internal/notify/
  notify.go          // platform-agnostic: snapshot diff + Process()
  notify_darwin.go   // fire() via osascript          (//go:build darwin)
  notify_other.go    // fire() no-op on other OSes     (//go:build !darwin)
  notify_test.go     // table-driven tests for Process() and escaping
```

#### Notifiable transitions

A session triggers a notification when its current state **differs from the last
notified state** and the current state is one of:

| State                       | Message                  | Sound  |
|-----------------------------|--------------------------|--------|
| `done` (`StateDone`)        | `[<name>] done!`         | `Tink` |
| `question` (`StateWaiting`) | `[<name>] needs your input` | `Funk` |

`<name>` resolves with the same priority the UI uses for the label column:
`Name` (user-defined) → `Summary` → `basename(CWD)`. Implemented as a small
`displayLabel(sessions.Session) string` helper in the package.

#### Persisted snapshot

File: `~/.tsession/notify.json`

```json
{ "entries": { "<session-id>": "done" } }
```

A flat map of session ID → last-notified state string. Lives alongside the
other `~/.tsession` runtime files (`runtime.json`, `cache.json`, `names.json`).

#### `Process(sessions []sessions.Session)`

Read-modify-write under a file lock (advisory `flock` on the snapshot file, or a
sidecar `notify.lock`) so concurrent `--notify` observers serialize:

1. Load the snapshot map (missing file → empty map).
2. For each session, compute its **notifiable state**: `"done"`, `"question"`,
   or `""` (anything else).
3. Decision per session ID:
   - **Not present in the map** → first sighting. Record the current notifiable
     state **without firing** (prevents a startup flood for sessions already
     sitting in `done`/`question`).
   - **Present, and current notifiable state differs from stored** → update the
     stored value, and if the new state is `done`/`question`, `fire()` the
     corresponding message + sound. (Transition into `done`/`question`; also
     covers `done → active → done` correctly because the stored value changed
     away from `done` in between.)
   - **Present and unchanged** → no-op (no re-fire while it stays `done`).
4. Optionally prune entries for session IDs no longer present in the input (keeps
   the file from growing without bound; a re-appearing ID then counts as a first
   sighting, which is fine).
5. Save atomically (temp file + rename), then release the lock.

The file lock guarantees exactly-once firing across a `watch --daemon --notify`
ticking every 10s and a `browse --watch --notify` reloading every 5s: the first
to observe a transition updates the snapshot; the other sees the already-updated
value and does not re-fire.

This snapshot is **independent of `donestate`**. `donestate` continues to drive
`StateDone` rendering and is still consumed by every load; the notifier keeps its
own last-notified map updated only by `--notify` callers.

#### `fire(title, sound string)` — platform split

- **`notify_darwin.go`**: runs
  `osascript -e 'display notification "<title>" sound name "<sound>"'`.
  The title contains arbitrary user text (Name/Summary), so it is escaped for
  AppleScript string context: backslash `\` → `\\` and double-quote `"` → `\"`
  before interpolation. osascript is invoked synchronously (it returns in a few
  tens of ms; the number of simultaneous transitions per tick is small), so a
  short-lived `list` process completes the notification before exiting.
- **`notify_other.go`**: `fire` is a no-op.

### Command wiring (`--notify`, default off)

- **`watch`** (`cmd/watch.go`)
  - Add `--notify` bool flag.
  - Propagate it through `spawnDaemon` into the re-exec'd child's args (so the
    detached daemon keeps the flag).
  - In `refresh`, when notify is enabled, call `notify.Process(allSessions)`
    after the merged list is assembled (local + remote), before/after writing the
    cache (order doesn't matter; keep it after the cache write).

- **`list`** (`cmd/list.go`)
  - Add `--notify` bool flag.
  - After `loadAll(...)` produces the session slice and before printing, call
    `notify.Process(sessions)` when the flag is set. This is the path
    `browse --watch` actually drives via repeated `list --fzf` subprocesses.

- **`browse`** (`cmd/browse.go`)
  - Add `--notify` bool flag.
  - When set, append `--notify` to the `reloadCmd` string used by the fzf
    `--reload`/`ctrl-r`/auto-refresh bindings, so every reload runs
    `tsession list --fzf … --notify` and diffs/fires.

### Scope of sessions

All tracked sessions, local and remote. Both already appear in the merged list
that `watch` and `list` build, so no extra work — `Process` simply receives the
full slice.

## Error handling

- Snapshot load/save and lock errors are best-effort and non-fatal: a failure to
  notify must never break `list`/`browse`/`watch`. Log to stderr at most; return
  no error to the caller.
- `osascript` failures (e.g. running headless) are ignored.
- On non-macOS, `fire` is compiled out to a no-op; `Process` still maintains the
  snapshot (harmless) but nothing is displayed.

## Testing

Unit tests in `internal/notify` with an injectable `fire` capture (function
variable defaulting to the real `fire`, overridden in tests to record calls):

- no transition (state stays `working`) → no fire.
- `working → done` (first sighting already recorded as `working`) → one fire,
  `Tink`.
- `working → question` → one fire, `Funk`.
- **first sighting** of a session already `done` → recorded, **no fire**.
- repeated `done` across two `Process` calls → fires once only.
- `done → active → done` → fires on each entry into `done`.
- `displayLabel` priority: Name → Summary → basename(CWD).
- AppleScript escaping: titles containing `"` and `\` are escaped correctly.

The real macOS `fire` (side-effecting `osascript`) is not unit-tested.

Validate with `go build .` and `go test ./...` from the repo root.

## Documentation

Update `README.md` and `AGENTS.md` to document the `--notify` flag on `watch`,
`list`, and `browse`, including the macOS-only caveat and the
`~/.tsession/notify.json` snapshot file.
