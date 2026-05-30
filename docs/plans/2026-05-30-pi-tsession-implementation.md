# Pi + tsession Integration Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Enable tsession to discover pi sessions alongside Copilot CLI sessions, with working/question/done/exited state tracking and tmux pane correlation.

**Architecture:** A pi extension writes per-session state files to `~/.tsession/pi-state/<id>.json`. A new `internal/pisessions` package in tsession reads those files. The existing `loadAllLive()` merges both sources into the unified session list.

**Tech Stack:** TypeScript (pi extension), Go (tsession), JSON (state file interchange)

---

## Phase 1: Pi Extension

### Task 1: Create the pi extension scaffold

**TDD scenario:** New feature — but this is a pi extension (TypeScript, no test runner in pi's extension system). We verify manually by running pi and checking the state file appears.

**Files:**
- Create: `~/.pi/agent/extensions/tsession-state.ts`

**Step 1: Write the extension**

```typescript
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { writeFileSync, mkdirSync, readdirSync, unlinkSync, statSync, renameSync } from "node:fs";
import { join, basename } from "node:path";
import { tmpdir, homedir } from "node:os";

const STATE_DIR = join(homedir(), ".tsession", "pi-state");

interface PiState {
  id: string;
  state: "working" | "question" | "done" | "idle" | "exited";
  cwd: string;
  summary: string;
  updatedAt: string;
  pid: number;
  sessionFile: string;
}

export default function (pi: ExtensionAPI) {
  let sessionId: string | undefined;
  let cwd: string | undefined;
  let sessionFile: string | undefined;

  function writeState(state: PiState["state"], ctx?: ExtensionContext) {
    if (!sessionId) return;
    mkdirSync(STATE_DIR, { recursive: true });

    let summary = "";
    if (ctx) {
      summary = pi.getSessionName() ?? "";
      if (!summary) {
        // Use first user message as summary
        for (const entry of ctx.sessionManager.getEntries()) {
          if (entry.type === "message" && entry.message?.role === "user") {
            const content = entry.message.content;
            if (typeof content === "string") {
              summary = content.slice(0, 100);
            } else if (Array.isArray(content)) {
              const text = content.find((c: any) => c.type === "text");
              if (text) summary = (text as any).text.slice(0, 100);
            }
            break;
          }
        }
      }
    }

    const data: PiState = {
      id: sessionId,
      state,
      cwd: cwd ?? "",
      summary,
      updatedAt: new Date().toISOString(),
      pid: process.pid,
      sessionFile: sessionFile ?? "",
    };

    const filePath = join(STATE_DIR, `${sessionId}.json`);
    const tmpPath = join(STATE_DIR, `.${sessionId}.tmp`);
    writeFileSync(tmpPath, JSON.stringify(data, null, 2));
    renameSync(tmpPath, filePath);
  }

  function getLastAssistantText(ctx: ExtensionContext): string {
    const entries = ctx.sessionManager.getEntries();
    for (let i = entries.length - 1; i >= 0; i--) {
      const entry = entries[i];
      if (entry.type === "message" && entry.message?.role === "assistant") {
        const content = entry.message.content;
        if (Array.isArray(content)) {
          for (let j = content.length - 1; j >= 0; j--) {
            const block = content[j] as any;
            if (block.type === "text" && block.text) {
              return block.text.trimEnd();
            }
          }
        }
        break;
      }
    }
    return "";
  }

  function cleanupStaleFiles() {
    try {
      const files = readdirSync(STATE_DIR);
      const now = Date.now();
      for (const file of files) {
        if (!file.endsWith(".json")) continue;
        const filePath = join(STATE_DIR, file);
        try {
          const stat = statSync(filePath);
          // Remove exited files older than 1 hour
          if (now - stat.mtimeMs > 3600_000) {
            const raw = JSON.parse(
              require("node:fs").readFileSync(filePath, "utf-8")
            );
            if (raw.state === "exited") {
              unlinkSync(filePath);
            }
          }
        } catch {}
      }
    } catch {}
  }

  pi.on("session_start", async (_event, ctx) => {
    cwd = ctx.cwd;
    const sf = ctx.sessionManager.getSessionFile?.();
    sessionFile = sf ?? "";
    // Extract session ID from filename: <timestamp>_<uuid>.jsonl
    if (sf) {
      const base = basename(sf, ".jsonl");
      const underscoreIdx = base.indexOf("_");
      sessionId = underscoreIdx >= 0 ? base.slice(underscoreIdx + 1) : base;
    }
    writeState("idle", ctx);
    cleanupStaleFiles();
  });

  pi.on("turn_start", async (_event, ctx) => {
    writeState("working", ctx);
  });

  pi.on("agent_end", async (_event, ctx) => {
    const lastText = getLastAssistantText(ctx);
    const state = lastText.endsWith("?") ? "question" : "done";
    writeState(state, ctx);
  });

  pi.on("session_shutdown", async (_event, ctx) => {
    writeState("exited", ctx);
  });
}
```

**Step 2: Verify it works**

```bash
mkdir -p ~/.tsession/pi-state
pi  # start a pi session, send a message, check:
ls ~/.tsession/pi-state/
cat ~/.tsession/pi-state/*.json
```

Expected: A JSON file appears with the session's state.

**Step 3: Commit**

```bash
cd ~/.pi/agent/extensions
git init  # if not already tracked — or just note it's manually placed
```

Note: This extension lives outside the tsession repo. No git commit needed in tsession for this file.

---

## Phase 2: tsession — Pi Session Discovery

### Task 2: Create `internal/pisessions` package with tests

**TDD scenario:** New feature — full TDD cycle.

**Files:**
- Create: `internal/pisessions/pisessions.go`
- Create: `internal/pisessions/pisessions_test.go`

**Step 1: Write the failing test**

```go
package pisessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

func TestLoadAll_ReadsStateFiles(t *testing.T) {
	dir := t.TempDir()

	state := stateFile{
		ID:          "abc-123",
		State:       "working",
		CWD:         "/Users/x/project",
		Summary:     "test summary",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		PID:         12345,
		SessionFile: "/path/to/session.jsonl",
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "abc-123.json"), data, 0o644)

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 session, got %d", len(got))
	}
	if got[0].ID != "abc-123" {
		t.Errorf("ID: want abc-123, got %s", got[0].ID)
	}
	if got[0].State != sessions.StateWorking {
		t.Errorf("State: want working, got %s", got[0].State)
	}
	if got[0].CWD != "/Users/x/project" {
		t.Errorf("CWD: want /Users/x/project, got %s", got[0].CWD)
	}
	if got[0].Summary != "test summary" {
		t.Errorf("Summary: want 'test summary', got %s", got[0].Summary)
	}
	if got[0].Source != "pi" {
		t.Errorf("Source: want pi, got %s", got[0].Source)
	}
}

func TestLoadAll_MapsStatesCorrectly(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		fileState string
		want      sessions.State
	}{
		{"working", sessions.StateWorking},
		{"question", sessions.StateWaiting},
		{"done", sessions.StateDone},
		{"idle", sessions.StateActiveIdle},
		{"exited", sessions.StateExited},
	}
	for _, c := range cases {
		state := stateFile{
			ID:        "id-" + c.fileState,
			State:     c.fileState,
			CWD:       "/x",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			PID:       1000,
		}
		data, _ := json.Marshal(state)
		os.WriteFile(filepath.Join(dir, "id-"+c.fileState+".json"), data, 0o644)
	}

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(cases) {
		t.Fatalf("want %d sessions, got %d", len(cases), len(got))
	}
	byID := map[string]sessions.Session{}
	for _, s := range got {
		byID[s.ID] = s
	}
	for _, c := range cases {
		s := byID["id-"+c.fileState]
		if s.State != c.want {
			t.Errorf("state %q: want %s, got %s", c.fileState, c.want, s.State)
		}
	}
}

func TestLoadAll_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, ".tmp.json"), []byte("{}"), 0o644)

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestLoadAll_MissingDirReturnsEmpty(t *testing.T) {
	got, err := loadFromDir("/nonexistent/path/xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestLoadAll_DetectsStalePID(t *testing.T) {
	dir := t.TempDir()

	state := stateFile{
		ID:        "stale-1",
		State:     "working",
		CWD:       "/x",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		PID:       999999999, // unlikely to be alive
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "stale-1.json"), data, 0o644)

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].State != sessions.StateExited {
		t.Errorf("stale PID should be exited, got %s", got[0].State)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/yarma/src/tsession.worktrees/pi
go test ./internal/pisessions/ -v
```

Expected: Compilation error — package doesn't exist yet.

**Step 3: Write minimal implementation**

```go
package pisessions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

type stateFile struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	CWD         string `json:"cwd"`
	Summary     string `json:"summary"`
	UpdatedAt   string `json:"updatedAt"`
	PID         int    `json:"pid"`
	SessionFile string `json:"sessionFile"`
}

// LoadAll reads all pi state files from ~/.tsession/pi-state/.
func LoadAll() ([]sessions.Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".tsession", "pi-state")
	return loadFromDir(dir)
}

func loadFromDir(dir string) ([]sessions.Session, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var out []sessions.Session
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var sf stateFile
		if err := json.Unmarshal(data, &sf); err != nil {
			continue
		}
		s := sessions.Session{
			ID:      sf.ID,
			CWD:     sf.CWD,
			Summary: sf.Summary,
			Source:  "pi",
		}
		s.UpdatedAt = parseTime(sf.UpdatedAt)
		s.LastEventAt = s.UpdatedAt
		s.State = mapState(sf.State)

		// Stale PID detection: if not exited, check if process alive
		if s.State != sessions.StateExited && sf.PID > 0 {
			if !isProcessAlive(sf.PID) {
				s.State = sessions.StateExited
			}
		}

		out = append(out, s)
	}
	return out, nil
}

func mapState(s string) sessions.State {
	switch s {
	case "working":
		return sessions.StateWorking
	case "question":
		return sessions.StateWaiting
	case "done":
		return sessions.StateDone
	case "idle":
		return sessions.StateActiveIdle
	case "exited":
		return sessions.StateExited
	default:
		return sessions.StateUnknown
	}
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/pisessions/ -v
```

Expected: All 5 tests PASS.

**Step 5: Commit**

```bash
git add internal/pisessions/
git commit -m "feat: add internal/pisessions package for pi state file discovery"
```

---

### Task 3: Add `Source` field to `sessions.Session`

**TDD scenario:** Modifying tested code — run existing tests first to confirm green.

**Files:**
- Modify: `internal/sessions/session.go`

**Step 1: Run existing tests**

```bash
go test ./internal/sessions/ -v
```

Expected: All PASS.

**Step 2: Add Source field**

In `internal/sessions/session.go`, add `Source` field to the `Session` struct:

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
	Source      string // "copilot" or "pi"
}
```

**Step 3: Run all tests**

```bash
go test ./... -v
```

Expected: All PASS (Source is just a new field, nothing breaks).

**Step 4: Commit**

```bash
git add internal/sessions/session.go
git commit -m "feat: add Source field to Session struct"
```

---

### Task 4: Integrate pi sessions into `loadAllLive()`

**TDD scenario:** Integration change — verify with manual testing and existing test suite.

**Files:**
- Modify: `cmd/watch.go` (the `loadAllLive` function)

**Step 1: Run existing tests**

```bash
go test ./... -v
```

Expected: All PASS.

**Step 2: Add pi session loading to `loadAllLive()`**

In `cmd/watch.go`, import `"github.com/yarma/tsession/internal/pisessions"` and append pi sessions after the Copilot merge:

```go
func loadAllLive(maxAge time.Duration) ([]sessions.Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".copilot", "session-store.db")
	stateRoot := filepath.Join(home, ".copilot", "session-state")

	store, err := sessions.LoadRecent(dbPath, maxAge)
	if err != nil {
		return nil, fmt.Errorf("load session store: %w", err)
	}
	ids := make([]string, len(store))
	for i, s := range store {
		ids[i] = s.ID
	}
	sd, err := sessions.LoadStateDirsForIDs(stateRoot, ids)
	if err != nil {
		return nil, fmt.Errorf("load state dirs: %w", err)
	}
	tx, err := tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list tmux: %w", err)
	}
	panes, _ := tmux.ListPanes()
	merged := sessions.Merge(store, sd, tx)
	merged = sessions.ResolveTmuxByPID(merged, sd, panes)

	// Mark Copilot sessions
	for i := range merged {
		if merged[i].Source == "" {
			merged[i].Source = "copilot"
		}
	}

	// Load pi sessions
	piSessions, err := pisessions.LoadAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: pi session load failed:", err)
	} else if len(piSessions) > 0 {
		// Filter by maxAge
		cutoff := time.Now().Add(-maxAge)
		for _, ps := range piSessions {
			if !ps.UpdatedAt.IsZero() && ps.UpdatedAt.Before(cutoff) {
				continue
			}
			merged = append(merged, ps)
		}
	}

	return merged, nil
}
```

**Step 3: Run all tests**

```bash
go test ./...
```

Expected: All PASS. (loadAllLive isn't directly unit-tested — it's integration code.)

**Step 4: Manual verification**

```bash
go build -o /tmp/tsession . && /tmp/tsession list --no-cache
```

Expected: If pi state files exist in `~/.tsession/pi-state/`, they appear in the list.

**Step 5: Commit**

```bash
git add cmd/watch.go
git commit -m "feat: integrate pi sessions into loadAllLive"
```

---

### Task 5: PID-based tmux matching for pi sessions

**TDD scenario:** Modifying integration code — need to pass pi PIDs through the existing `ResolveTmuxByPID` path.

**Files:**
- Modify: `cmd/watch.go` (pass pi PIDs to tmux resolution)

**Step 1: Update `loadAllLive()` to resolve pi tmux by PID**

After appending pi sessions, build `StateDirInfo` entries for them and run `ResolveTmuxByPID`:

```go
	// Load pi sessions
	piSessions, err := pisessions.LoadAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: pi session load failed:", err)
	} else if len(piSessions) > 0 {
		cutoff := time.Now().Add(-maxAge)
		var piFiltered []sessions.Session
		for _, ps := range piSessions {
			if !ps.UpdatedAt.IsZero() && ps.UpdatedAt.Before(cutoff) {
				continue
			}
			piFiltered = append(piFiltered, ps)
		}
		// Build StateDirInfo for PID-based tmux matching
		piSD := pisessions.StateDirInfos(piFiltered)
		piFiltered = sessions.ResolveTmuxByPID(piFiltered, piSD, panes)
		merged = append(merged, piFiltered...)
	}
```

Add a helper to `internal/pisessions/pisessions.go`:

```go
// StateDirInfos builds StateDirInfo entries from pi sessions for tmux PID matching.
// It re-reads the state files to get the PID. We store PID in Session temporarily.
func StateDirInfos(sess []sessions.Session) []sessions.StateDirInfo {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".tsession", "pi-state")
	var out []sessions.StateDirInfo
	for _, s := range sess {
		data, err := os.ReadFile(filepath.Join(dir, s.ID+".json"))
		if err != nil {
			continue
		}
		var sf stateFile
		if err := json.Unmarshal(data, &sf); err != nil {
			continue
		}
		out = append(out, sessions.StateDirInfo{
			ID:  s.ID,
			PID: sf.PID,
		})
	}
	return out
}
```

**Step 2: Run all tests**

```bash
go test ./...
```

Expected: All PASS.

**Step 3: Commit**

```bash
git add cmd/watch.go internal/pisessions/pisessions.go
git commit -m "feat: PID-based tmux matching for pi sessions"
```

---

### Task 6: Resume behavior for pi sessions

**TDD scenario:** Modifying existing code — run tests first.

**Files:**
- Modify: `cmd/resume.go`

**Step 1: Run existing tests**

```bash
go test ./...
```

Expected: All PASS.

**Step 2: Update Resume to handle pi sessions**

```go
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yarma/tsession/internal/donestate"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func Resume(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession resume <session-id>")
	}
	id := args[0]

	merged, err := loadAll(14*24*time.Hour, false)
	if err != nil {
		return err
	}
	var match *sessions.Session
	for i := range merged {
		if merged[i].ID == id {
			match = &merged[i]
			break
		}
	}

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

	// Pi session without tmux match: use `pi --session`
	if match != nil && match.Source == "pi" {
		cmd := exec.Command("pi", "--session", id)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}

	// Copilot session without tmux match: use `copilot --resume`
	if _, err := exec.LookPath("copilot"); err != nil {
		fmt.Println(id)
		return nil
	}
	cmd := exec.Command("copilot", "--resume="+id)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
```

**Step 3: Run all tests**

```bash
go test ./...
```

Expected: All PASS.

**Step 4: Commit**

```bash
git add cmd/resume.go
git commit -m "feat: resume pi sessions with 'pi --session'"
```

---

### Task 7: Render differentiation for pi sessions

**TDD scenario:** Trivial change — add a source indicator to the render output.

**Files:**
- Modify: `internal/render/render.go`

**Step 1: Add source prefix to state glyph**

Update `stateGlyph` to accept the session (or add a source glyph function):

In `formatLine`, prepend a source indicator before the state:

```go
func sourceGlyph(source string) string {
	switch source {
	case "pi":
		return "π"
	default:
		return "©"
	}
}
```

Then in `formatLine`, change:
```go
state := s.State.String()
```
to:
```go
src := sourceGlyph(s.Source)
state := s.State.String()
```

And update the format string to include `src` before `state`:
```go
// In the non-short format:
display := fmt.Sprintf("  %s%s %-5s %-16s %-30s %-80s  %s",
    src, state, age, ...
// In the short format:
display := fmt.Sprintf("  %s%s %-5s %-20s %-30s",
    src, state, age, ...
```

**Step 2: Run render tests**

```bash
go test ./internal/render/ -v
```

Expected: May need to update test expectations for the new prefix.

**Step 3: Commit**

```bash
git add internal/render/render.go
git commit -m "feat: show source indicator (π/©) in session list"
```

---

### Task 8: Done-state integration for pi sessions

**TDD scenario:** The existing `donestate` + `Merge` logic already handles done state by session ID. Pi sessions bypass `Merge` (they come pre-classified). We need to apply the same done-state logic.

**Files:**
- Modify: `cmd/watch.go` (apply donestate to pi sessions)

**Step 1: Apply donestate transitions to pi sessions**

After loading pi sessions and before appending, apply the done-state runtime logic. The simplest approach: include pi sessions in `Merge` by passing them through a wrapper, OR inline the done-state logic:

```go
// After piFiltered is built, apply done-state transitions
rt, _ := donestate.Load()
if rt == nil {
    rt = &donestate.File{Entries: map[string]donestate.Entry{}}
}
now := time.Now()
dirty := false
for i := range piFiltered {
    s := &piFiltered[i]
    entry := rt.Entries[s.ID]
    prev := entry.LastState
    hadDone := !entry.DoneSince.IsZero()
    raw := s.State

    switch {
    case raw == sessions.StateActiveIdle:
        if prev == sessions.StateWorking.String() {
            entry.DoneSince = now
            hadDone = true
        }
        if hadDone {
            s.State = sessions.StateDone
        }
    default:
        if hadDone {
            entry.DoneSince = time.Time{}
        }
    }
    if entry.LastState != raw.String() {
        entry.LastState = raw.String()
        rt.Entries[s.ID] = entry
        dirty = true
    }
}
if dirty {
    _ = donestate.Save(rt)
}
```

Wait — actually, the pi extension already classifies `done` and `question` directly. The `donestate` logic in tsession is only needed for Copilot because Copilot doesn't distinguish done from idle. Pi's extension already writes `"done"` explicitly based on the `?` heuristic.

So we actually DON'T need donestate logic for pi sessions — the extension handles it. The only thing we need is for `donestate.Clear(id)` in `Resume` to work (which it already does — it's ID-based).

But we DO still need donestate for the "clear on pane switch" behavior. The extension writes `done` → tsession reads it. When you switch to the pane, tsession should write back that done is cleared... but it can't modify the pi state file.

**Revised approach:** The done-state clearing for pi needs the pi extension to detect "user is now interacting" and transition done→idle. This happens naturally: when the user types the next message, `turn_start` fires → state becomes `working`. So "done" clears itself on next interaction.

The tmux-pane-switch clearing is nice-to-have but not essential for pi — the user will type something and it'll go to `working` immediately. We skip this for now.

**Step 1: No code change needed**

The pi extension's done/question distinction already works end-to-end. Skip this task.

**Step 2: Commit** — nothing to commit, this task is complete by analysis.

---

## Phase 2 Summary

After all tasks, run full verification:

```bash
cd /Users/yarma/src/tsession.worktrees/pi
go test ./...
go build -o /tmp/tsession .
/tmp/tsession list --no-cache
```

Then test with a live pi session:
1. Open pi in one tmux pane
2. Send a message (state should go working → done/question)
3. Run `tsession list --no-cache` — pi session should appear
4. Run `tsession browse` — pi session should be selectable
5. Select the pi session — should switch to correct tmux pane
