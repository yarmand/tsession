# Remote tmux Session Support

**Date:** 2026-05-27
**Branch:** yarma/remote

## Problem

`tsession` currently only shows sessions from the local machine. Users who run Copilot CLI sessions on remote machines (via SSH) have no way to see those sessions alongside their local ones. The goal is to display both local and remote sessions in the list/browse/popup views, separated into distinct visual sections.

## Assumptions

- Remote machines are accessed via SSH (standard `ssh host` connectivity).
- The remote machine runs tmux + Copilot CLI with the same `~/.copilot/` directory structure.
- `tsession` binary is NOT required on the remote — we gather data over SSH.
- A user configures one or more remotes in a config file.
- "Resume" for a remote session means SSH-ing into the remote and attaching to the tmux pane (or running `copilot --resume`).

## Design

### Configuration

A YAML config file at `~/.config/tsession/config.yaml`:

```yaml
remotes:
  - name: devbox          # Display label for the section header
    host: devbox.local    # SSH host (as in ~/.ssh/config or user@host)
    copilot_dir: ~/.copilot  # Optional, defaults to ~/.copilot
```

Multiple remotes are supported; each becomes its own section in the list.

### Data Gathering

For each configured remote, `tsession` executes a single SSH command that gathers all needed data in one round-trip:

```bash
ssh <host> 'cat ~/.copilot/session-store.db' > /tmp/tsession-remote-<name>.db
```

Actually, SQLite over `cat` is fragile. Better approach — run a lightweight gather script over SSH that emits JSON:

**Remote gather protocol:** `tsession` ships a small self-contained gather script (embedded in the binary) that it pipes to `ssh <host> bash -s`. The script:

1. Queries `session-store.db` using `sqlite3` CLI (widely available) for recent sessions.
2. For each session ID, reads the last 64KB of `events.jsonl` and the `workspace.yaml`.
3. Runs `tmux list-sessions` and `tmux list-panes`.
4. Outputs a single JSON blob to stdout.

This avoids needing `tsession` installed on the remote. Requires only `sqlite3`, `bash`, and `tmux` on the remote.

**Requirement:** The remote must have `sqlite3` and `bash` available. If `sqlite3` is missing, the remote is skipped with a warning (similar to SSH failure). This keeps the implementation simple.

**Gather script JSON output schema:**

```json
{
  "sessions": [
    {
      "id": "uuid",
      "cwd": "/path/to/dir",
      "repository": "github.com/org/repo",
      "summary": "session summary text",
      "updated_at": "2006-01-02T15:04:05Z"
    }
  ],
  "state_dirs": [
    {
      "id": "uuid",
      "cwd": "/path",
      "last_event_type": "assistant.turn_end",
      "last_event_tool": "",
      "last_event_at": "2006-01-02T15:04:05Z",
      "events_tail": "... last 64KB of events.jsonl as a string ..."
    }
  ],
  "tmux_sessions": [
    { "name": "myproject", "path": "/home/user/myproject" }
  ],
  "tmux_panes": [
    { "session_name": "myproject", "window_index": "0", "pane_index": "0", "pid": 12345 }
  ],
  "process_tree": { "12345": 12300, "12300": 1 }
}
```

### Session Model Changes

Add an `Origin` field to `sessions.Session`:

```go
type Session struct {
    // ... existing fields ...
    Origin      string // "" = local, otherwise remote name from config
}
```

### Merge & Sort

Remote sessions go through the same `Merge()` logic (state classification from events, tmux matching against the remote's tmux state). The sort groups become:

1. **Local sessions** — existing bucket sort (tmux-attached → active → idle → exited)
2. **Remote sessions** — grouped by remote name, same internal bucket sort

Within the fzf picker, sections are separated by a visual divider line.

### Display

#### List output (stdout)

```
── Local ──────────────────────────────────────────────────────────
  ● working  2m  tsession    Fix browse layout               abc123
  ○ active   1h  myproject   Add auth module                 def456
── devbox ─────────────────────────────────────────────────────────
  ● working  5m  backend     Implement caching               789abc
  · idle     3h  infra       Terraform refactor              012def
```

The section headers are rendered with ANSI dim/bold styling when color is enabled.

#### fzf picker (browse/popup)

Section dividers are tab-delimited lines where field 2 (the ID) is empty. Since `--accept-nth=2` returns an empty string, the resume logic treats it as "no selection." The display field shows a styled separator like `── devbox ──`. This requires no special fzf features.

### Resume Behavior

When resuming a remote session:

1. If the session has a `TmuxTarget` on the remote → `ssh -t <host> tmux attach -t <target>`
2. If no tmux match → `ssh -t <host> copilot --resume=<id>`

This opens an interactive SSH session in the current terminal/pane.

### Caching

The watcher (`tsession watch`) gains remote support:
- Each refresh cycle gathers remote data in parallel with local data.
- Remote data is included in `cache.json` with the `Origin` field set.
- SSH failures for a remote produce a warning in the watch log but don't block local data.
- A per-remote timeout (default 10s) prevents slow/unreachable hosts from blocking the refresh cycle.

### CLI Changes

New global flag: `--local-only` — skip remote gathering (useful when SSH is unavailable or for speed).

The `--active` filter applies within each section independently.

### Error Handling

- SSH connection failure: skip that remote, emit a warning line in the output (e.g., `── devbox (unreachable) ──`).
- `sqlite3` not found on remote: skip with warning.
- Partial data (e.g., events.jsonl missing): show the session with `unknown` state.

### Performance Budget

- Local load: unchanged (~200ms).
- Remote load: one SSH round-trip per remote, target <2s per host.
- With caching enabled: remote data comes from cache, no SSH on each `list` call.

## Non-Goals (YAGNI)

- Syncing/mirroring session state between machines.
- Running `tsession watch` on the remote.
- Two-way communication (e.g., sending input to a remote session from local).
- Auto-discovery of remotes (explicit config only).
- Remote support without SSH (no HTTP API, no agents).

## Testing Strategy

- Unit tests for the gather script JSON parsing.
- Unit tests for config file loading.
- Integration test with a mock SSH command that returns canned JSON.
- Render tests for section dividers in both list and fzf modes.
