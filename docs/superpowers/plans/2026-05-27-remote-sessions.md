# Remote tmux Session Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display remote Copilot CLI sessions (gathered over SSH) alongside local ones, grouped into separate visual sections.

**Architecture:** A config file defines SSH remotes. An embedded bash gather script runs over SSH to collect session data as JSON. The existing merge/render pipeline processes remote sessions identically, with an `Origin` field to group them by section. Resume for remote sessions opens an interactive SSH connection.

**Tech Stack:** Go 1.25, SSH (os/exec), embedded bash script (embed package), YAML config (hand-parsed to avoid dependency), JSON parsing for remote data.

---

## File Structure

| Path | Action | Responsibility |
|------|--------|---------------|
| `internal/config/config.go` | Create | Load `~/.config/tsession/config.yaml`, parse remote definitions |
| `internal/config/config_test.go` | Create | Unit tests for config parsing |
| `internal/remote/gather.bash` | Create | Embedded bash script that runs on the remote host |
| `internal/remote/remote.go` | Create | SSH execution, JSON parsing of gather output, timeout handling |
| `internal/remote/remote_test.go` | Create | Unit tests for JSON parsing, mock SSH for integration |
| `internal/sessions/session.go` | Modify | Add `Origin` field |
| `internal/sessions/merge.go` | Modify | Accept remote sessions, group by origin in sort |
| `internal/render/render.go` | Modify | Add section divider rendering |
| `internal/render/render_test.go` | Modify | Tests for section dividers |
| `cmd/list.go` | Modify | Load config, gather remote sessions, render sections |
| `cmd/browse.go` | Modify | Include section dividers in fzf feed |
| `cmd/resume.go` | Modify | Handle remote resume (SSH -t) |
| `cmd/watch.go` | Modify | Gather remote data in parallel during refresh |
| `README.md` | Modify | Document remote configuration and usage |

---

### Task 1: Configuration Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test for config parsing**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRemotes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`remotes:
  - name: devbox
    host: devbox.local
  - name: server
    host: user@server.example.com
    copilot_dir: /home/user/.copilot
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Remotes) != 2 {
		t.Fatalf("got %d remotes, want 2", len(cfg.Remotes))
	}
	if cfg.Remotes[0].Name != "devbox" {
		t.Errorf("remote[0].Name = %q, want devbox", cfg.Remotes[0].Name)
	}
	if cfg.Remotes[0].Host != "devbox.local" {
		t.Errorf("remote[0].Host = %q, want devbox.local", cfg.Remotes[0].Host)
	}
	if cfg.Remotes[0].CopilotDir != "~/.copilot" {
		t.Errorf("remote[0].CopilotDir = %q, want ~/.copilot (default)", cfg.Remotes[0].CopilotDir)
	}
	if cfg.Remotes[1].CopilotDir != "/home/user/.copilot" {
		t.Errorf("remote[1].CopilotDir = %q, want /home/user/.copilot", cfg.Remotes[1].CopilotDir)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatal("expected no error for missing file")
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("got %d remotes, want 0 for missing file", len(cfg.Remotes))
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte(""), 0o644)

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("got %d remotes, want 0", len(cfg.Remotes))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement config loading**

```go
// internal/config/config.go
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Remote struct {
	Name       string
	Host       string
	CopilotDir string
}

type Config struct {
	Remotes []Remote
}

// Load reads the default config at ~/.config/tsession/config.yaml.
// Returns an empty config (no error) if the file does not exist.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Config{}, nil
	}
	return LoadFrom(filepath.Join(home, ".config", "tsession", "config.yaml"))
}

// LoadFrom reads config from a specific path.
// Returns an empty config (no error) if the file does not exist.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	return parse(string(data))
}

// parse does minimal YAML parsing for our flat structure.
// We avoid a YAML dependency since the format is simple and stable.
func parse(s string) (*Config, error) {
	cfg := &Config{}
	lines := strings.Split(s, "\n")

	var current *Remote
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if trimmed == "remotes:" {
			continue
		}

		if strings.HasPrefix(trimmed, "- name:") {
			if current != nil {
				cfg.Remotes = append(cfg.Remotes, *current)
			}
			current = &Remote{
				Name:       extractValue(trimmed[len("- name:"):]),
				CopilotDir: "~/.copilot",
			}
			continue
		}

		if current != nil && indent >= 4 {
			if strings.HasPrefix(trimmed, "host:") {
				current.Host = extractValue(trimmed[len("host:"):])
			} else if strings.HasPrefix(trimmed, "copilot_dir:") {
				v := extractValue(trimmed[len("copilot_dir:"):])
				if v != "" {
					current.CopilotDir = v
				}
			}
		}
	}
	if current != nil {
		cfg.Remotes = append(cfg.Remotes, *current)
	}
	return cfg, nil
}

func extractValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add config loading for remote definitions"
```

---

### Task 2: Add Origin Field to Session Model

**Files:**
- Modify: `internal/sessions/session.go`

- [ ] **Step 1: Add Origin field to Session struct**

In `internal/sessions/session.go`, add `Origin string` after `TmuxTarget`:

```go
type Session struct {
	ID          string
	CWD         string
	Repository  string
	Summary     string
	UpdatedAt   time.Time
	LastEventAt time.Time
	State       State
	TmuxName    string
	TmuxTarget  string
	Origin      string // "" = local, otherwise remote name from config
}
```

- [ ] **Step 2: Run existing tests to verify nothing breaks**

Run: `go test ./...`
Expected: PASS (adding a field doesn't break anything)

- [ ] **Step 3: Commit**

```bash
git add internal/sessions/session.go
git commit -m "feat(sessions): add Origin field for remote session support"
```

---

### Task 3: Remote Gather Script

**Files:**
- Create: `internal/remote/gather.bash`

- [ ] **Step 1: Write the gather bash script**

```bash
#!/usr/bin/env bash
# tsession remote gather script
# Reads Copilot session data and outputs a JSON blob.
# Requirements: bash, sqlite3, tmux (optional)
# Arguments: $1 = copilot_dir (default: ~/.copilot), $2 = max_age_hours (default: 336)
set -euo pipefail

COPILOT_DIR="${1:-$HOME/.copilot}"
MAX_AGE_HOURS="${2:-336}"

DB="$COPILOT_DIR/session-store.db"
STATE_DIR="$COPILOT_DIR/session-state"

# Check sqlite3
if ! command -v sqlite3 &>/dev/null; then
  echo '{"error":"sqlite3 not found"}' 
  exit 0
fi

# Check DB exists
if [ ! -f "$DB" ]; then
  echo '{"sessions":[],"state_dirs":[],"tmux_sessions":[],"tmux_panes":[],"process_tree":{}}'
  exit 0
fi

# Query sessions
CUTOFF=$(date -u -d "-${MAX_AGE_HOURS} hours" '+%Y-%m-%d %H:%M:%S' 2>/dev/null || \
         date -u -v-${MAX_AGE_HOURS}H '+%Y-%m-%d %H:%M:%S' 2>/dev/null || \
         echo "2000-01-01 00:00:00")

SESSIONS_JSON=$(sqlite3 -json "$DB" \
  "SELECT id, COALESCE(cwd,'') as cwd, COALESCE(repository,'') as repository, COALESCE(summary,'') as summary, updated_at FROM sessions WHERE updated_at >= '$CUTOFF' ORDER BY updated_at DESC;" 2>/dev/null || echo '[]')

# If sqlite3 -json not supported, fall back
if [ "$SESSIONS_JSON" = "" ] || ! echo "$SESSIONS_JSON" | head -c1 | grep -q '\['; then
  SESSIONS_JSON="[]"
fi

# Gather state dirs for each session
STATE_DIRS_JSON="["
FIRST=true
for ID in $(echo "$SESSIONS_JSON" | grep -o '"id":"[^"]*"' | cut -d'"' -f4); do
  DIR="$STATE_DIR/$ID"
  [ -d "$DIR" ] || continue

  # Read workspace.yaml cwd
  CWD=""
  if [ -f "$DIR/workspace.yaml" ]; then
    CWD=$(grep '^cwd:' "$DIR/workspace.yaml" 2>/dev/null | head -1 | sed 's/^cwd: *//' | tr -d '"'"'" || true)
  fi

  # Read last events (tail 64KB)
  EVENTS_TAIL=""
  EVFILE="$DIR/events.jsonl"
  if [ -f "$EVFILE" ]; then
    EVENTS_TAIL=$(tail -c 65536 "$EVFILE" 2>/dev/null || true)
  fi

  # Read inuse PID
  PID=0
  for LOCKFILE in "$DIR"/inuse.*.lock; do
    [ -f "$LOCKFILE" ] || continue
    BASENAME=$(basename "$LOCKFILE")
    PID=$(echo "$BASENAME" | sed 's/inuse\.\([0-9]*\)\.lock/\1/')
    break
  done

  if [ "$FIRST" = true ]; then FIRST=false; else STATE_DIRS_JSON+=","; fi
  # Use jq if available, otherwise python, otherwise raw
  if command -v jq &>/dev/null; then
    STATE_DIRS_JSON+=$(jq -nc --arg id "$ID" --arg cwd "$CWD" --arg events "$EVENTS_TAIL" --argjson pid "$PID" \
      '{id:$id, cwd:$cwd, events_tail:$events, pid:$pid}')
  elif command -v python3 &>/dev/null; then
    STATE_DIRS_JSON+=$(python3 -c "
import json,sys
print(json.dumps({'id':'$ID','cwd':'$CWD','events_tail':sys.stdin.read(),'pid':$PID}))" <<< "$EVENTS_TAIL")
  else
    # Minimal escape — replace backslashes, quotes, newlines
    ESC_EVENTS=$(printf '%s' "$EVENTS_TAIL" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' '\036' | sed 's/\x1e/\\n/g')
    ESC_CWD=$(printf '%s' "$CWD" | sed 's/\\/\\\\/g; s/"/\\"/g')
    STATE_DIRS_JSON+="{\"id\":\"$ID\",\"cwd\":\"$ESC_CWD\",\"events_tail\":\"$ESC_EVENTS\",\"pid\":$PID}"
  fi
done
STATE_DIRS_JSON+="]"

# Gather tmux sessions
TMUX_SESSIONS_JSON="[]"
if command -v tmux &>/dev/null; then
  TMUX_RAW=$(tmux list-sessions -F '#{session_name}|#{session_path}' 2>/dev/null || true)
  if [ -n "$TMUX_RAW" ]; then
    TMUX_SESSIONS_JSON="["
    FIRST=true
    while IFS='|' read -r NAME PATH; do
      if [ "$FIRST" = true ]; then FIRST=false; else TMUX_SESSIONS_JSON+=","; fi
      TMUX_SESSIONS_JSON+="{\"name\":\"$NAME\",\"path\":\"$PATH\"}"
    done <<< "$TMUX_RAW"
    TMUX_SESSIONS_JSON+="]"
  fi
fi

# Gather tmux panes
TMUX_PANES_JSON="[]"
if command -v tmux &>/dev/null; then
  PANES_RAW=$(tmux list-panes -a -F '#{session_name}|#{window_index}|#{pane_index}|#{pane_pid}' 2>/dev/null || true)
  if [ -n "$PANES_RAW" ]; then
    TMUX_PANES_JSON="["
    FIRST=true
    while IFS='|' read -r SNAME WIDX PIDX PID; do
      if [ "$FIRST" = true ]; then FIRST=false; else TMUX_PANES_JSON+=","; fi
      TMUX_PANES_JSON+="{\"session_name\":\"$SNAME\",\"window_index\":\"$WIDX\",\"pane_index\":\"$PIDX\",\"pid\":$PID}"
    done <<< "$PANES_RAW"
    TMUX_PANES_JSON+="]"
  fi
fi

# Gather process tree (pid -> ppid)
PTREE_JSON="{}"
PS_RAW=$(ps -A -o pid=,ppid= 2>/dev/null || true)
if [ -n "$PS_RAW" ]; then
  PTREE_JSON="{"
  FIRST=true
  while read -r P PP; do
    [ -n "$P" ] && [ -n "$PP" ] || continue
    if [ "$FIRST" = true ]; then FIRST=false; else PTREE_JSON+=","; fi
    PTREE_JSON+="\"$P\":$PP"
  done <<< "$PS_RAW"
  PTREE_JSON+="}"
fi

# Output final JSON
cat <<EOF
{"sessions":$SESSIONS_JSON,"state_dirs":$STATE_DIRS_JSON,"tmux_sessions":$TMUX_SESSIONS_JSON,"tmux_panes":$TMUX_PANES_JSON,"process_tree":$PTREE_JSON}
EOF
```

- [ ] **Step 2: Commit the gather script**

```bash
git add internal/remote/gather.bash
git commit -m "feat(remote): add gather bash script for remote data collection"
```

---

### Task 4: Remote Data Fetching and Parsing

**Files:**
- Create: `internal/remote/remote.go`
- Create: `internal/remote/remote_test.go`

- [ ] **Step 1: Write failing tests for JSON parsing**

```go
// internal/remote/remote_test.go
package remote

import (
	"testing"
	"time"
)

func TestParseGatherOutput(t *testing.T) {
	input := `{"sessions":[{"id":"abc-123","cwd":"/home/user/project","repository":"github.com/org/repo","summary":"Fix bug","updated_at":"2026-05-27 10:00:00"}],"state_dirs":[{"id":"abc-123","cwd":"/home/user/project","events_tail":"{\"type\":\"assistant.turn_end\",\"timestamp\":\"2026-05-27T10:05:00Z\"}\n","pid":1234}],"tmux_sessions":[{"name":"project","path":"/home/user/project"}],"tmux_panes":[{"session_name":"project","window_index":"0","pane_index":"0","pid":1234}],"process_tree":{"1234":1000,"1000":1}}`

	result, err := ParseGatherOutput([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(result.Sessions))
	}
	if result.Sessions[0].ID != "abc-123" {
		t.Errorf("session ID = %q, want abc-123", result.Sessions[0].ID)
	}
	if result.Sessions[0].CWD != "/home/user/project" {
		t.Errorf("session CWD = %q", result.Sessions[0].CWD)
	}
	if len(result.StateDirs) != 1 {
		t.Fatalf("got %d state_dirs, want 1", len(result.StateDirs))
	}
	if result.StateDirs[0].PID != 1234 {
		t.Errorf("state_dir PID = %d, want 1234", result.StateDirs[0].PID)
	}
	if len(result.TmuxSessions) != 1 {
		t.Fatalf("got %d tmux_sessions, want 1", len(result.TmuxSessions))
	}
	if len(result.TmuxPanes) != 1 {
		t.Fatalf("got %d tmux_panes, want 1", len(result.TmuxPanes))
	}
	if result.ProcessTree[1234] != 1000 {
		t.Errorf("process_tree[1234] = %d, want 1000", result.ProcessTree[1234])
	}
}

func TestParseGatherOutputError(t *testing.T) {
	input := `{"error":"sqlite3 not found"}`
	_, err := ParseGatherOutput([]byte(input))
	if err == nil {
		t.Fatal("expected error for error response")
	}
}

func TestParseGatherOutputEmpty(t *testing.T) {
	input := `{"sessions":[],"state_dirs":[],"tmux_sessions":[],"tmux_panes":[],"process_tree":{}}`
	result, err := ParseGatherOutput([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 0 {
		t.Fatalf("got %d sessions, want 0", len(result.Sessions))
	}
}

func TestGatherResultToSessions(t *testing.T) {
	result := &GatherResult{
		Sessions: []GatherSession{
			{ID: "abc-123", CWD: "/home/user/project", Repository: "github.com/org/repo", Summary: "Fix bug", UpdatedAt: "2026-05-27 10:00:00"},
		},
		StateDirs: []GatherStateDir{
			{ID: "abc-123", CWD: "/home/user/project", EventsTail: "{\"type\":\"session.shutdown\",\"timestamp\":\"2026-05-27T10:05:00Z\"}\n", PID: 0},
		},
		TmuxSessions: []GatherTmuxSession{
			{Name: "project", Path: "/home/user/project"},
		},
		TmuxPanes:   []GatherTmuxPane{},
		ProcessTree: map[int]int{},
	}

	sessions := result.ToSessions("devbox", time.Duration(0))
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].Origin != "devbox" {
		t.Errorf("Origin = %q, want devbox", sessions[0].Origin)
	}
	if sessions[0].TmuxName != "project" {
		t.Errorf("TmuxName = %q, want project", sessions[0].TmuxName)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/remote/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement remote.go**

```go
// internal/remote/remote.go
package remote

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

//go:embed gather.bash
var gatherScript string

type GatherSession struct {
	ID         string `json:"id"`
	CWD        string `json:"cwd"`
	Repository string `json:"repository"`
	Summary    string `json:"summary"`
	UpdatedAt  string `json:"updated_at"`
}

type GatherStateDir struct {
	ID         string `json:"id"`
	CWD        string `json:"cwd"`
	EventsTail string `json:"events_tail"`
	PID        int    `json:"pid"`
}

type GatherTmuxSession struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type GatherTmuxPane struct {
	SessionName string `json:"session_name"`
	WindowIndex string `json:"window_index"`
	PaneIndex   string `json:"pane_index"`
	PID         int    `json:"pid"`
}

type GatherResult struct {
	Error        string              `json:"error,omitempty"`
	Sessions     []GatherSession     `json:"sessions"`
	StateDirs    []GatherStateDir    `json:"state_dirs"`
	TmuxSessions []GatherTmuxSession `json:"tmux_sessions"`
	TmuxPanes    []GatherTmuxPane    `json:"tmux_panes"`
	ProcessTree  map[int]int         `json:"process_tree"`
}

func ParseGatherOutput(data []byte) (*GatherResult, error) {
	var result GatherResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse gather output: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("remote error: %s", result.Error)
	}
	return &result, nil
}

// Fetch runs the gather script on a remote host via SSH and returns parsed results.
func Fetch(ctx context.Context, remote config.Remote, maxAge time.Duration) (*GatherResult, error) {
	hours := int(maxAge.Hours())
	if hours < 1 {
		hours = 336
	}
	script := fmt.Sprintf("bash -s -- %s %d", remote.CopilotDir, hours)

	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", remote.Host, script)
	cmd.Stdin = strings.NewReader(gatherScript)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh %s: %w (stderr: %s)", remote.Host, err, stderr.String())
	}

	return ParseGatherOutput(stdout.Bytes())
}

// FetchAll gathers data from all configured remotes in parallel.
// Returns a map of remote-name → sessions. Failures are logged to warnings.
func FetchAll(ctx context.Context, remotes []config.Remote, maxAge time.Duration, timeout time.Duration) (map[string][]sessions.Session, []string) {
	type result struct {
		name     string
		sessions []sessions.Session
		err      error
	}

	ch := make(chan result, len(remotes))
	for _, r := range remotes {
		go func(remote config.Remote) {
			rctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			gr, err := Fetch(rctx, remote, maxAge)
			if err != nil {
				ch <- result{name: remote.Name, err: err}
				return
			}
			ch <- result{name: remote.Name, sessions: gr.ToSessions(remote.Name, maxAge)}
		}(r)
	}

	out := map[string][]sessions.Session{}
	var warnings []string
	for range remotes {
		r := <-ch
		if r.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", r.name, r.err))
			continue
		}
		out[r.name] = r.sessions
	}
	return out, warnings
}

// ToSessions converts GatherResult into merged sessions with Origin set.
func (gr *GatherResult) ToSessions(origin string, maxAge time.Duration) []sessions.Session {
	store := make([]sessions.Session, 0, len(gr.Sessions))
	for _, gs := range gr.Sessions {
		s := sessions.Session{
			ID:         gs.ID,
			CWD:        gs.CWD,
			Repository: gs.Repository,
			Summary:    gs.Summary,
			UpdatedAt:  parseSqliteTime(gs.UpdatedAt),
			Origin:     origin,
		}
		store = append(store, s)
	}

	// Build state dir infos by parsing events_tail
	sd := make([]sessions.StateDirInfo, 0, len(gr.StateDirs))
	for _, gsd := range gr.StateDirs {
		info := sessions.StateDirInfo{
			ID:  gsd.ID,
			CWD: gsd.CWD,
			PID: gsd.PID,
		}
		if gsd.EventsTail != "" {
			state, lastEventAt := classifyRemoteEvents(gsd.EventsTail)
			info.State = state
			info.LastEventAt = lastEventAt
		}
		sd = append(sd, info)
	}

	// Build tmux data
	tmuxSessions := make([]tmux.Session, 0, len(gr.TmuxSessions))
	for _, ts := range gr.TmuxSessions {
		tmuxSessions = append(tmuxSessions, tmux.Session{Name: ts.Name, Path: ts.Path})
	}

	panes := make([]tmux.Pane, 0, len(gr.TmuxPanes))
	for _, tp := range gr.TmuxPanes {
		pid, _ := strconv.Atoi(fmt.Sprint(tp.PID))
		panes = append(panes, tmux.Pane{
			SessionName: tp.SessionName,
			WindowIndex: tp.WindowIndex,
			PaneIndex:   tp.PaneIndex,
			PID:         pid,
		})
	}

	merged := sessions.MergeRemote(store, sd, tmuxSessions, gr.ProcessTree)
	merged = sessions.ResolveTmuxByPIDWithTree(merged, sd, panes, gr.ProcessTree)
	return merged
}

// classifyRemoteEvents parses events tail and returns state + last event time.
func classifyRemoteEvents(tail string) (sessions.State, time.Time) {
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	if len(lines) == 0 {
		return sessions.StateUnknown, time.Time{}
	}

	type ev struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Data      struct {
			ToolName string `json:"toolName"`
		} `json:"data"`
	}

	var parsed []ev
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e ev
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		parsed = append(parsed, e)
	}
	if len(parsed) == 0 {
		return sessions.StateUnknown, time.Time{}
	}

	last := parsed[len(parsed)-1]
	lastTS, _ := time.Parse(time.RFC3339Nano, last.Timestamp)

	// Check for shutdown
	for _, e := range parsed {
		if e.Type == "session.shutdown" {
			return sessions.StateExited, lastTS
		}
	}

	// Check for unmatched prompting tool
	completed := 0
	for i := len(parsed) - 1; i >= 0; i-- {
		e := parsed[i]
		switch e.Type {
		case "tool.execution_complete":
			completed++
		case "tool.execution_start":
			if completed > 0 {
				completed--
				continue
			}
			if isPromptingTool(e.Data.ToolName) {
				return sessions.StateWaiting, lastTS
			}
		case "tool.user_requested":
			return sessions.StateWaiting, lastTS
		}
	}

	// Check turn start/end
	var lastStart, lastEnd time.Time
	for _, e := range parsed {
		ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
		switch e.Type {
		case "assistant.turn_start":
			lastStart = ts
		case "assistant.turn_end":
			lastEnd = ts
		}
	}
	if !lastStart.IsZero() && lastStart.After(lastEnd) {
		return sessions.StateWorking, lastTS
	}

	// For remote, we can't check lsof — treat as ActiveIdle if there's a PID
	return sessions.StateActiveIdle, lastTS
}

func isPromptingTool(name string) bool {
	switch name {
	case "ask_user", "ask_question", "request_permission":
		return true
	}
	return false
}

func parseSqliteTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// filepath is imported for potential path operations
var _ = filepath.Base
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remote/ -v`
Expected: PASS (may need adjustments for MergeRemote/ResolveTmuxByPIDWithTree which are in next task)

- [ ] **Step 5: Commit**

```bash
git add internal/remote/
git commit -m "feat(remote): add SSH gather execution and JSON parsing"
```

---

### Task 5: Extend Merge for Remote Sessions

**Files:**
- Modify: `internal/sessions/merge.go`
- Modify: `internal/sessions/tmuxmatch.go`

- [ ] **Step 1: Write failing test for MergeRemote**

Add to an existing test file or create new:

```go
// internal/sessions/merge_test.go (add to existing)
func TestMergeRemoteSetsOrigin(t *testing.T) {
	store := []Session{
		{ID: "r1", CWD: "/home/user/proj", Origin: "devbox", UpdatedAt: time.Now()},
	}
	sd := []StateDirInfo{
		{ID: "r1", State: StateWorking, LastEventAt: time.Now()},
	}
	tmuxSessions := []tmux.Session{
		{Name: "proj", Path: "/home/user/proj"},
	}
	ptree := map[int]int{}

	merged := MergeRemote(store, sd, tmuxSessions, ptree)
	if len(merged) != 1 {
		t.Fatalf("got %d, want 1", len(merged))
	}
	if merged[0].Origin != "devbox" {
		t.Errorf("Origin = %q, want devbox", merged[0].Origin)
	}
	if merged[0].TmuxName != "proj" {
		t.Errorf("TmuxName = %q, want proj", merged[0].TmuxName)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sessions/ -run TestMergeRemote -v`
Expected: FAIL — `MergeRemote` undefined.

- [ ] **Step 3: Implement MergeRemote**

Add to `internal/sessions/merge.go`:

```go
// MergeRemote is like Merge but for remote sessions. It skips donestate
// tracking (remote "done" state is ephemeral per-fetch) and uses a provided
// process tree instead of building one locally.
func MergeRemote(store []Session, stateDirs []StateDirInfo, tmuxSessions []tmux.Session, processTree map[int]int) []Session {
	stateByID := map[string]StateDirInfo{}
	for _, s := range stateDirs {
		stateByID[s.ID] = s
	}
	tmuxByPath := map[string]string{}
	tmuxByBase := map[string]string{}
	for _, t := range tmuxSessions {
		if t.Path != "" {
			tmuxByPath[t.Path] = t.Name
		}
		tmuxByBase[t.Name] = t.Name
	}

	out := make([]Session, 0, len(store))
	for _, s := range store {
		if sd, ok := stateByID[s.ID]; ok {
			s.State = sd.State
			s.LastEventAt = sd.LastEventAt
			if sd.CWD != "" {
				s.CWD = sd.CWD
			}
		}
		if name, ok := tmuxByPath[s.CWD]; ok {
			s.TmuxName = name
		} else if s.CWD != "" {
			if name, ok := tmuxByBase[filepath.Base(s.CWD)]; ok {
				s.TmuxName = name
			}
		}
		out = append(out, s)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ba, bb := bucket(a), bucket(b); ba != bb {
			return ba < bb
		}
		if pa, pb := statePriority(a.State), statePriority(b.State); pa != pb {
			return pa > pb
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
	return out
}
```

- [ ] **Step 4: Implement ResolveTmuxByPIDWithTree in tmuxmatch.go**

Add to `internal/sessions/tmuxmatch.go`:

```go
// ResolveTmuxByPIDWithTree is like ResolveTmuxByPID but uses a pre-built
// process tree (for remote sessions where we can't run ps locally).
func ResolveTmuxByPIDWithTree(sess []Session, sd []StateDirInfo, panes []tmux.Pane, processTree map[int]int) []Session {
	if len(panes) == 0 || len(processTree) == 0 {
		return sess
	}
	pidBySession := map[string]int{}
	for _, s := range sd {
		if s.PID > 0 {
			pidBySession[s.ID] = s.PID
		}
	}
	if len(pidBySession) == 0 {
		return sess
	}

	paneByPID := map[int]tmux.Pane{}
	for _, p := range panes {
		paneByPID[p.PID] = p
	}

	for i := range sess {
		if sess[i].TmuxName != "" {
			continue
		}
		pid, ok := pidBySession[sess[i].ID]
		if !ok {
			continue
		}
		if pane, ok := walkToPane(pid, processTree, paneByPID); ok {
			sess[i].TmuxName = pane.SessionName
			sess[i].TmuxTarget = pane.Target()
		}
	}
	return sess
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/sessions/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/sessions/merge.go internal/sessions/tmuxmatch.go internal/sessions/merge_test.go
git commit -m "feat(sessions): add MergeRemote and ResolveTmuxByPIDWithTree"
```

---

### Task 6: Section Divider Rendering

**Files:**
- Modify: `internal/render/render.go`
- Modify: `internal/render/render_test.go`

- [ ] **Step 1: Write failing test for section divider**

```go
// Add to internal/render/render_test.go
func TestFormatSectionDivider(t *testing.T) {
	got := FormatSectionDivider("devbox", true, 0)
	if !strings.Contains(got, "devbox") {
		t.Errorf("divider missing name: %q", got)
	}
	if !strings.Contains(got, "──") {
		t.Errorf("divider missing box chars: %q", got)
	}
	// Should have tab + empty ID for fzf
	if !strings.HasSuffix(got, "\t") {
		t.Errorf("divider should end with tab (empty ID): %q", got)
	}
}

func TestFormatSectionDividerNoColor(t *testing.T) {
	got := FormatSectionDivider("Local", false, 0)
	if strings.Contains(got, "\x1b[") {
		t.Error("no-color divider contains ANSI escape")
	}
	if !strings.Contains(got, "Local") {
		t.Errorf("divider missing name: %q", got)
	}
}

func TestFormatSectionDividerLshort(t *testing.T) {
	got := FormatSectionDivider("devbox", false, 20)
	runes := []rune(strings.Split(got, "\t")[0])
	if len(runes) > 20 {
		t.Errorf("lshort divider has %d runes, want <=20", len(runes))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/ -run TestFormatSectionDivider -v`
Expected: FAIL — `FormatSectionDivider` undefined.

- [ ] **Step 3: Implement FormatSectionDivider**

Add to `internal/render/render.go`:

```go
// FormatSectionDivider renders a visual separator line for grouping sessions
// by origin (local vs remote). The returned string is tab-delimited with an
// empty second field so fzf's --accept-nth=2 returns "" (no-op on selection).
func FormatSectionDivider(name string, color bool, lshort int) string {
	label := "── " + name + " "
	// Pad with box-drawing to a reasonable width
	targetWidth := 60
	if lshort > 0 && lshort < targetWidth {
		targetWidth = lshort
	}
	for len([]rune(label)) < targetWidth {
		label += "─"
	}
	if lshort > 0 {
		runes := []rune(label)
		if len(runes) > lshort {
			label = string(runes[:lshort])
		}
	}
	if color {
		label = "\x1b[1;90m" + label + "\x1b[0m"
	}
	return label + "\t"
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/render/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/render/render.go internal/render/render_test.go
git commit -m "feat(render): add section divider for local/remote grouping"
```

---

### Task 7: Integrate Remote Sessions into List/Browse

**Files:**
- Modify: `cmd/list.go`
- Modify: `cmd/browse.go`

- [ ] **Step 1: Add `--local-only` flag and remote loading to loadAll**

Modify `cmd/list.go` — add a helper that loads both local and remote:

```go
// At the top of list.go, add imports for:
// "context"
// "github.com/yarma/tsession/internal/config"
// "github.com/yarma/tsession/internal/remote"

// loadAllWithRemotes loads local sessions and, unless localOnly is set,
// also gathers from configured remotes. Returns local sessions, a map of
// remote-name→sessions, ordered remote names, and any warnings.
func loadAllWithRemotes(maxAge time.Duration, noCache bool, localOnly bool) ([]sessions.Session, map[string][]sessions.Session, []string, []string, error) {
	local, err := loadAll(maxAge, noCache)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if localOnly {
		return local, nil, nil, nil, nil
	}

	cfg, err := config.Load()
	if err != nil || len(cfg.Remotes) == 0 {
		return local, nil, nil, nil, nil
	}

	ctx := context.Background()
	remoteMap, warnings := remote.FetchAll(ctx, cfg.Remotes, maxAge, 10*time.Second)

	// Preserve config order
	names := make([]string, 0, len(cfg.Remotes))
	for _, r := range cfg.Remotes {
		if _, ok := remoteMap[r.Name]; ok {
			names = append(names, r.Name)
		}
	}
	return local, remoteMap, names, warnings, nil
}
```

- [ ] **Step 2: Update List() to render sections**

Modify the `List` function to use `loadAllWithRemotes` and render section headers:

```go
func List(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	fzfMode := fs.Bool("fzf", false, "emit tab-delimited lines for fzf consumption")
	noCache := fs.Bool("no-cache", false, "ignore the watch cache and load live")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	localOnly := fs.Bool("local-only", false, "skip remote session gathering")
	_ = fs.Parse(args)

	local, remoteMap, remoteNames, warnings, err := loadAllWithRemotes(*maxAge, *noCache, *localOnly)
	if err != nil {
		return err
	}

	hasRemotes := len(remoteMap) > 0

	if *active {
		local = filterActive(local)
	}

	useShort := *short || *lshort > 0
	if useShort {
		enrichOrigins(local)
	}

	color := !*noColor && !*fzfMode
	if *lshort > 0 {
		color = false
	}
	now := time.Now()

	// Build short context from ALL sessions (local + remote)
	var allSessions []sessions.Session
	allSessions = append(allSessions, local...)
	for _, name := range remoteNames {
		allSessions = append(allSessions, remoteMap[name]...)
	}
	var shortCtx render.ShortContext
	if useShort {
		shortCtx = render.BuildShortContext(allSessions)
	}

	if !*fzfMode {
		header := render.Header
		if useShort {
			header = render.HeaderShort
		}
		if *lshort > 0 {
			header = truncateRunes(header, *lshort)
		}
		if color {
			fmt.Fprintln(os.Stdout, "\x1b[1;34m"+header+"\x1b[0m")
		} else {
			fmt.Fprintln(os.Stdout, header)
		}
	}

	// Print section divider for "Local" only when remotes exist
	if hasRemotes && !*fzfMode {
		fmt.Fprintln(os.Stdout, render.FormatSectionDivider("Local", color, *lshort))
	} else if hasRemotes && *fzfMode {
		fmt.Fprintln(os.Stdout, render.FormatSectionDivider("Local", false, *lshort))
	}

	// Render local sessions (same as before)
	renderSessionList(os.Stdout, local, now, color, useShort, *fzfMode, *lshort, shortCtx)

	// Render remote sections
	for _, name := range remoteNames {
		remoteSessions := remoteMap[name]
		if *active {
			remoteSessions = filterActive(remoteSessions)
		}
		if len(remoteSessions) == 0 {
			continue
		}
		fmt.Fprintln(os.Stdout, render.FormatSectionDivider(name, color && !*fzfMode, *lshort))
		renderSessionList(os.Stdout, remoteSessions, now, color, useShort, *fzfMode, *lshort, shortCtx)
	}

	// Print warnings for unreachable remotes
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	return nil
}
```

- [ ] **Step 3: Extract renderSessionList helper**

```go
func renderSessionList(w *os.File, merged []sessions.Session, now time.Time, color, useShort, fzfMode bool, lshort int, shortCtx render.ShortContext) {
	for _, s := range merged {
		if useShort {
			line := render.FormatLineShortWithContext(s, now, color, shortCtx, lshort)
			parts := strings.SplitN(line, "\t", 2)
			display, id := parts[0], ""
			if len(parts) == 2 {
				id = parts[1]
			}

			if fzfMode {
				ts := s.LastEventAt
				if ts.IsZero() {
					ts = s.UpdatedAt
				}
				age := render.FormatAge(now.Sub(ts))
				summary := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " "), "\t", " ")
				if summary == "" {
					summary = "(no summary)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					display, id, s.Repository, s.CWD,
					render.OriginShortName(s.Repository),
					s.State.String(), age, summary, shortCtx.LegendField(),
				)
			} else {
				fmt.Fprintln(w, display)
			}
		} else {
			fmt.Fprintln(w, render.FormatLine(s, now, color))
		}
	}
}
```

- [ ] **Step 4: Update browse.go initialListBytes to include remotes**

Modify `initialListBytes` in `cmd/browse.go` to also call `loadAllWithRemotes` and render section dividers in the fzf feed.

- [ ] **Step 5: Run tests and manual verification**

Run: `go build . && go test ./...`
Expected: PASS, builds successfully.

- [ ] **Step 6: Commit**

```bash
git add cmd/list.go cmd/browse.go
git commit -m "feat: integrate remote sessions into list and browse output"
```

---

### Task 8: Remote Resume

**Files:**
- Modify: `cmd/resume.go`

- [ ] **Step 1: Write failing test for remote resume path**

Since Resume does exec, test the logic path selection:

```go
// cmd/resume_test.go
package cmd

import (
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestResumeCommandForRemote(t *testing.T) {
	s := sessions.Session{
		ID:         "abc-123",
		Origin:     "devbox",
		TmuxTarget: "project:0.0",
	}
	args := remoteResumeArgs(s, "devbox.local")
	if len(args) < 4 {
		t.Fatalf("expected at least 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "ssh" {
		t.Errorf("args[0] = %q, want ssh", args[0])
	}
	if args[1] != "-t" {
		t.Errorf("args[1] = %q, want -t", args[1])
	}
	if args[2] != "devbox.local" {
		t.Errorf("args[2] = %q, want devbox.local", args[2])
	}
	expected := "tmux attach -t project:0.0"
	if args[3] != expected {
		t.Errorf("args[3] = %q, want %q", args[3], expected)
	}
}

func TestResumeCommandForRemoteNoTmux(t *testing.T) {
	s := sessions.Session{
		ID:     "abc-123",
		Origin: "devbox",
	}
	args := remoteResumeArgs(s, "devbox.local")
	expected := "copilot --resume=abc-123"
	if args[3] != expected {
		t.Errorf("args[3] = %q, want %q", args[3], expected)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestResumeCommand -v`
Expected: FAIL — `remoteResumeArgs` undefined.

- [ ] **Step 3: Implement remote resume logic**

Add to `cmd/resume.go`:

```go
// remoteResumeArgs builds the SSH command args for resuming a remote session.
func remoteResumeArgs(s sessions.Session, host string) []string {
	target := s.TmuxTarget
	if target == "" {
		target = s.TmuxName
	}
	var remoteCmd string
	if target != "" {
		remoteCmd = "tmux attach -t " + target
	} else {
		remoteCmd = "copilot --resume=" + s.ID
	}
	return []string{"ssh", "-t", host, remoteCmd}
}
```

Update `Resume()` to detect remote sessions and use SSH:

```go
func Resume(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession resume <session-id>")
	}
	id := args[0]

	merged, err := loadAll(14*24*time.Hour, false)
	if err != nil {
		return err
	}

	// Also check remote sessions
	cfg, _ := config.Load()
	var match *sessions.Session
	var matchHost string

	for i := range merged {
		if merged[i].ID == id {
			match = &merged[i]
			break
		}
	}

	// If not found locally, check cache (which includes remote sessions)
	if match == nil && cfg != nil {
		if f, err := cache.Read(); err == nil {
			for i := range f.Sessions {
				if f.Sessions[i].ID == id {
					match = &f.Sessions[i]
					break
				}
			}
		}
	}

	// Find the host for remote sessions
	if match != nil && match.Origin != "" && cfg != nil {
		for _, r := range cfg.Remotes {
			if r.Name == match.Origin {
				matchHost = r.Host
				break
			}
		}
	}

	// Remote resume
	if match != nil && match.Origin != "" && matchHost != "" {
		cmdArgs := remoteResumeArgs(*match, matchHost)
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}

	// Local resume (existing logic)
	if match != nil && (match.TmuxTarget != "" || match.TmuxName != "") {
		target := match.TmuxTarget
		if target == "" {
			target = match.TmuxName
		}
		if err := tmux.SwitchClient(target); err != nil {
			return err
		}
		_ = donestate.Clear(id)
		return nil
	}

	if _, err := exec.LookPath("copilot"); err != nil {
		fmt.Println(id)
		return nil
	}
	cmd := exec.Command("copilot", "--resume="+id)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -run TestResumeCommand -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/resume.go cmd/resume_test.go
git commit -m "feat(resume): support SSH-based resume for remote sessions"
```

---

### Task 9: Watcher Remote Support

**Files:**
- Modify: `cmd/watch.go`

- [ ] **Step 1: Update refresh() to include remote data**

```go
func refresh(interval, maxAge time.Duration) error {
	local, err := loadAllLive(maxAge)
	if err != nil {
		return err
	}

	// Gather remote sessions in parallel
	cfg, _ := config.Load()
	var allSessions []sessions.Session
	allSessions = append(allSessions, local...)

	if cfg != nil && len(cfg.Remotes) > 0 {
		ctx := context.Background()
		remoteMap, warnings := remote.FetchAll(ctx, cfg.Remotes, maxAge, 10*time.Second)
		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		for _, r := range cfg.Remotes {
			if sessions, ok := remoteMap[r.Name]; ok {
				allSessions = append(allSessions, sessions...)
			}
		}
	}

	return cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  interval,
		Sessions:  allSessions,
	})
}
```

- [ ] **Step 2: Add necessary imports to watch.go**

Add `"context"`, `"github.com/yarma/tsession/internal/config"`, and `"github.com/yarma/tsession/internal/remote"` to the import block.

- [ ] **Step 3: Run build**

Run: `go build .`
Expected: Compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/watch.go
git commit -m "feat(watch): gather remote sessions during cache refresh"
```

---

### Task 10: Update README Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add Remote Sessions section to README**

Add the following after the "State legend" section at the end of `README.md`:

```markdown
## Remote Sessions

Display Copilot CLI sessions running on remote machines alongside your local sessions.

### Setup

Create `~/.config/tsession/config.yaml`:

```yaml
remotes:
  - name: devbox          # Section label in the picker
    host: devbox.local    # SSH host (as in ~/.ssh/config or user@host)
  - name: server
    host: user@server.example.com
    copilot_dir: /home/user/.copilot  # Optional, defaults to ~/.copilot
```

**Requirements on the remote:**
- `bash` and `sqlite3` must be available in PATH
- `tmux` (optional — enables pane-level matching)
- SSH must be configured for passwordless access (key-based auth)

### How it works

`tsession` runs a lightweight gather script over SSH that collects session data
from the remote's `~/.copilot/` directory and tmux state. Data is returned as
JSON in a single SSH round-trip. Each remote appears as its own section in the
list output:

```
── Local ──────────────────────────────────────────────────────────
  ● working  2m  tsession    Fix browse layout
  ○ active   1h  myproject   Add auth module
── devbox ─────────────────────────────────────────────────────────
  ● working  5m  backend     Implement caching
  · idle     3h  infra       Terraform refactor
```

### Resume behavior

Selecting a remote session opens an interactive SSH connection:
- If the session is attached to a tmux pane: `ssh -t <host> tmux attach -t <target>`
- Otherwise: `ssh -t <host> copilot --resume=<id>`

### Flags

| Flag           | Description                                      |
|----------------|--------------------------------------------------|
| `--local-only` | Skip remote gathering (useful offline or for speed) |

### Caching

When `tsession watch` is running, remote data is gathered alongside local data
on each refresh cycle. Each remote has a 10-second timeout — unreachable hosts
are skipped with a warning without blocking the local cache update.

### Troubleshooting

- **Remote unreachable:** The section shows as `── devbox (unreachable) ──` and
  local sessions work normally.
- **sqlite3 not found:** The remote is skipped. Install `sqlite3` on the remote.
- **Slow SSH:** Ensure `ControlMaster` is configured in `~/.ssh/config` for
  persistent connections. The gather script completes in <1s on most hosts.
```

- [ ] **Step 2: Verify README renders correctly**

Run: `cat README.md | head -150`
Visual inspection that formatting is correct.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add Remote Sessions section to README"
```

---

### Task 11: Integration Test with Mock SSH

**Files:**
- Create: `cmd/list_remote_test.go`

- [ ] **Step 1: Write integration test**

```go
// cmd/list_remote_test.go
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/sessions"
)

func TestRemoteSessionsInOutput(t *testing.T) {
	// Simulate parsed gather output
	gr := &remote.GatherResult{
		Sessions: []remote.GatherSession{
			{
				ID:         "remote-uuid-1",
				CWD:        "/home/user/backend",
				Repository: "github.com/org/backend",
				Summary:    "Implement caching layer",
				UpdatedAt:  time.Now().UTC().Format("2006-01-02 15:04:05"),
			},
		},
		StateDirs: []remote.GatherStateDir{
			{
				ID:         "remote-uuid-1",
				CWD:        "/home/user/backend",
				EventsTail: `{"type":"assistant.turn_start","timestamp":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}` + "\n",
				PID:        0,
			},
		},
		TmuxSessions: []remote.GatherTmuxSession{
			{Name: "backend", Path: "/home/user/backend"},
		},
		TmuxPanes:   []remote.GatherTmuxPane{},
		ProcessTree: map[int]int{},
	}

	remoteSessions := gr.ToSessions("devbox", 14*24*time.Hour)
	if len(remoteSessions) != 1 {
		t.Fatalf("got %d remote sessions, want 1", len(remoteSessions))
	}
	if remoteSessions[0].Origin != "devbox" {
		t.Errorf("Origin = %q, want devbox", remoteSessions[0].Origin)
	}
	if remoteSessions[0].State != sessions.StateWorking {
		t.Errorf("State = %v, want StateWorking", remoteSessions[0].State)
	}
	if remoteSessions[0].TmuxName != "backend" {
		t.Errorf("TmuxName = %q, want backend", remoteSessions[0].TmuxName)
	}
}

func TestConfigLoadIntegration(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".config", "tsession")
	_ = os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte(`remotes:
  - name: devbox
    host: devbox.local
`), 0o644)

	// Verify the file is parseable
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "devbox") {
		t.Fatal("config file doesn't contain expected content")
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `go test ./cmd/ -run TestRemote -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/list_remote_test.go
git commit -m "test: add integration tests for remote session loading"
```

---

### Task 12: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests PASS.

- [ ] **Step 2: Build the binary**

Run: `go build -o tsession .`
Expected: Successful build with no errors.

- [ ] **Step 3: Verify help output includes new flag**

Run: `./tsession list -h`
Expected: Shows `--local-only` in the flag list.

- [ ] **Step 4: Commit any final fixups**

```bash
git add -A
git status  # should be clean or have only formatting fixes
git commit -m "chore: final cleanup for remote session support" --allow-empty
```
