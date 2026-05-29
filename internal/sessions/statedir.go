// internal/sessions/statedir.go
package sessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yarma/tsession/internal/lsofutil"
)

type StateDirInfo struct {
	ID          string
	State       State
	LastEventAt time.Time
	DBLocked    bool
	CWD         string
	PID         int
}

type eventLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Data      struct {
		ToolName string `json:"toolName"`
	} `json:"data"`
}

// LoadAllStateDirs scans every subdirectory under root. Retained for callers
// that don't have a candidate ID set up front. Most code should use
// LoadStateDirsForIDs, which is dramatically cheaper.
func LoadAllStateDirs(root string) ([]StateDirInfo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return LoadStateDirsForIDs(root, ids)
}

// LoadStateDirsForIDs visits exactly the given session IDs under root,
// reads the last event line of each, and runs a single batched lsof to
// determine which session.db files are currently locked.
//
// Missing directories are silently skipped. Per-directory parsing runs
// across a small worker pool.
func LoadStateDirsForIDs(root string, ids []string) ([]StateDirInfo, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	type partial struct {
		idx       int
		info      StateDirInfo
		dbPath    string
		needsLsof bool
	}

	results := make([]partial, len(ids))

	workers := runtime.NumCPU()
	if workers > len(ids) {
		workers = len(ids)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan int, len(ids))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				id := ids[i]
				dir := filepath.Join(root, id)
				info := StateDirInfo{
					ID:  id,
					CWD: readWorkspaceCWD(filepath.Join(dir, "workspace.yaml")),
					PID: readInusePID(dir),
				}
				evs, ok := tailRecentEvents(filepath.Join(dir, "events.jsonl"))
				p := partial{idx: i, dbPath: filepath.Join(dir, "session.db")}
				if !ok {
					// No events yet — fall through to lsof/PID check to
					// determine if the session is live (brand-new) or idle.
					info.State = StateUnknown
					p.needsLsof = true
					p.info = info
					results[i] = p
					continue
				}
				last := evs[len(evs)-1]
				info.LastEventAt = last.TS
				state, needsLsof := classifyFromEvents(evs)
				info.State = state
				p.needsLsof = needsLsof
				p.info = info
				results[i] = p
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	// Batch lsof for all dirs that need the active-vs-inactive distinction.
	var lsofPaths []string
	for _, r := range results {
		if r.needsLsof {
			lsofPaths = append(lsofPaths, r.dbPath)
		}
	}
	var locked map[string]bool
	if len(lsofPaths) > 0 {
		l, err := lsofutil.LockedSet(lsofPaths)
		if err == nil {
			locked = l
		}
		// On lsof error we treat everything as not-locked → InactiveIdle.
		// That's better than failing the whole load.
	}

	out := make([]StateDirInfo, 0, len(results))
	for _, r := range results {
		if r.needsLsof {
			if locked[r.dbPath] {
				r.info.DBLocked = true
				r.info.State = StateActiveIdle
			} else if r.info.PID > 0 {
				// No session.db lock, but inuse.<pid>.lock exists — the
				// session is live (brand-new, no interaction yet).
				r.info.State = StateActiveIdle
			} else {
				r.info.State = StateInactiveIdle
			}
		}
		out = append(out, r.info)
	}
	return out, nil
}

// parsedEvent is the structured form of a single events.jsonl line.
type parsedEvent struct {
	Type     string
	ToolName string
	TS       time.Time
}

// tailRecentEvents returns the last few events of events.jsonl, in
// chronological order. It reads only the trailing window of the file so
// cost stays O(window) regardless of total file size. Returns (nil, false)
// if the file is missing or empty.
func tailRecentEvents(path string) ([]parsedEvent, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil, false
	}

	const window int64 = 64 * 1024
	size := st.Size()
	off := int64(0)
	if size > window {
		off = size - window
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil, false
	}
	// Drop a partial first line when we started mid-file.
	if off > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			return nil, false
		}
	}
	var out []parsedEvent
	for _, raw := range bytes.Split(buf, []byte("\n")) {
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		typ, tn, ts, ok := parseEventLine(line)
		if !ok {
			continue
		}
		out = append(out, parsedEvent{Type: typ, ToolName: tn, TS: ts})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// tailLastEvent is a thin wrapper that returns only the last parsed event,
// used by callers that don't need history context.
func tailLastEvent(path string) (eventType, toolName string, ts time.Time, ok bool) {
	evs, ok := tailRecentEvents(path)
	if !ok {
		return "", "", time.Time{}, false
	}
	e := evs[len(evs)-1]
	return e.Type, e.ToolName, e.TS, true
}

func parseEventLine(line string) (string, string, time.Time, bool) {
	if line == "" {
		return "", "", time.Time{}, false
	}
	var e eventLine
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		return "", "", time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	return e.Type, e.Data.ToolName, t.UTC(), true
}

// DiscoverLiveSessions finds session-state directories that have an
// inuse.<pid>.lock file but are NOT in the provided known set. This catches
// brand-new sessions that haven't yet appeared in session-store.db.
// Uses filepath.Glob for a single-pass scan rather than per-dir ReadDir.
func DiscoverLiveSessions(root string, knownIDs map[string]bool) []Session {
	pattern := filepath.Join(root, "*", "inuse.*.lock")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil
	}

	var out []Session
	for _, lockPath := range matches {
		dir := filepath.Dir(lockPath)
		id := filepath.Base(dir)
		if knownIDs[id] {
			continue
		}
		// Deduplicate if multiple lock files exist in the same dir.
		knownIDs[id] = true

		cwd := readWorkspaceCWD(filepath.Join(dir, "workspace.yaml"))
		var updatedAt time.Time
		if info, err := os.Stat(dir); err == nil {
			updatedAt = info.ModTime().UTC()
		}

		out = append(out, Session{
			ID:        id,
			CWD:       cwd,
			UpdatedAt: updatedAt,
		})
	}
	return out
}

// readInusePID looks for an `inuse.<pid>.lock` file in dir and returns the
// PID encoded in its name. The Copilot CLI writes this file while a session
// is being actively held by a process; the PID identifies the owning
// process, which we use to map the session back to a tmux pane.
func readInusePID(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "inuse.") || !strings.HasSuffix(name, ".lock") {
			continue
		}
		mid := name[len("inuse.") : len(name)-len(".lock")]
		if pid, err := strconv.Atoi(mid); err == nil {
			return pid
		}
	}
	return 0
}

// readWorkspaceCWD extracts the `cwd:` value from a session's workspace.yaml.
// The file is small and flat; we parse it manually to avoid pulling in a
// YAML dependency. Returns "" if the file is missing or has no cwd entry.
func readWorkspaceCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	// Read a small bounded amount — workspace.yaml is tiny.
	buf := make([]byte, 8192)
	n, _ := io.ReadFull(f, buf)
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "cwd:") {
			continue
		}
		v := strings.TrimSpace(trimmed[len("cwd:"):])
		v = strings.Trim(v, `"'`)
		return v
	}
	return ""
}

// classifyFromEvents derives a session state from the recent-event window.
//
// Decision order (first match wins):
//  1. session.shutdown anywhere in the window → Exited.
//  2. A `tool.execution_start` for a user-prompting tool (ask_user,
//     ask_question, request_permission) without a matching
//     tool.execution_complete after it → Waiting (rendered "question").
//  3. The most recent `assistant.turn_start` is newer than the most recent
//     `assistant.turn_end` → Working (the agent is mid-turn). This also
//     covers the common bash/edit case where tool.execution_start +
//     tool.execution_complete are followed by another tool start before the
//     turn ends.
//  4. The most recent `assistant.turn_end` is newer → unknown here, so we
//     fall through to ActiveIdle/InactiveIdle (caller checks db lock):
//     the agent has finished its turn and is waiting for the next user
//     input.
func classifyFromEvents(evs []parsedEvent) (State, bool) {
	for _, e := range evs {
		if e.Type == "session.shutdown" {
			return StateExited, false
		}
	}

	// Walk from the most recent event backward looking for an unmatched
	// user-prompting tool.execution_start, a tool.user_requested prompt,
	// or a permission.requested that has not yet been completed.
	// Stop at the most recent turn_end — events before it are history.
	completed := 0
	permCompleted := 0
	promptingStarts := 0
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		switch e.Type {
		case "assistant.turn_end":
			// Crossed into a previous turn — tool events before here
			// don't reflect current state.
			goto turnCheck
		case "tool.execution_complete":
			completed++
		case "permission.completed":
			permCompleted++
		case "permission.requested":
			if permCompleted > 0 {
				permCompleted--
				continue
			}
			return StateWaiting, false
		case "tool.user_requested":
			return StateWaiting, false
		case "tool.execution_start":
			if isUserPromptingTool(e.ToolName) {
				// User-prompting tools block until the user responds.
				// Each prompting start needs its own dedicated completion.
				// Completions for sibling non-prompting tools don't count.
				promptingStarts++
				continue
			}
			if completed > 0 {
				completed--
				continue
			}
		}
	}
turnCheck:
	// If there are unmatched prompting starts, check if enough completions
	// remain (after satisfying non-prompting tools) to cover them.
	if promptingStarts > 0 && completed < promptingStarts {
		return StateWaiting, false
	}

	var lastStart, lastEnd time.Time
	for _, e := range evs {
		switch e.Type {
		case "assistant.turn_start":
			lastStart = e.TS
		case "assistant.turn_end":
			lastEnd = e.TS
		}
	}
	if !lastStart.IsZero() && lastStart.After(lastEnd) {
		return StateWorking, false
	}

	// Fall back to the lsof-based active/inactive distinction for anything
	// after turn end (idle waiting for user input).
	return StateUnknown, true
}

// preliminaryState is retained for tests/callers that classify on a single
// event. It mirrors classifyFromEvents for single-event inputs.
func preliminaryState(eventType, toolName string) (State, bool) {
	switch {
	case eventType == "session.shutdown":
		return StateExited, false
	case strings.HasPrefix(eventType, "tool.execution_start"):
		if isUserPromptingTool(toolName) {
			return StateWaiting, false
		}
		return StateWorking, false
	case strings.HasPrefix(eventType, "agent.processing"),
		eventType == "assistant.turn_start":
		return StateWorking, false
	case eventType == "tool.user_requested",
		strings.Contains(eventType, "permission_request"),
		strings.Contains(eventType, "ask_question"):
		return StateWaiting, false
	default:
		return StateUnknown, true
	}
}

func isUserPromptingTool(name string) bool {
	switch name {
	case "ask_user", "ask_question", "request_permission":
		return true
	}
	return false
}

