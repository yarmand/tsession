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
				ev, ts, ok := tailLastEvent(filepath.Join(dir, "events.jsonl"))
				p := partial{idx: i, dbPath: filepath.Join(dir, "session.db")}
				if !ok {
					info.State = StateUnknown
					p.info = info
					results[i] = p
					continue
				}
				info.LastEventAt = ts
				state, needsLsof := preliminaryState(ev)
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
			} else {
				r.info.State = StateInactiveIdle
			}
		}
		out = append(out, r.info)
	}
	return out, nil
}

// tailLastEvent returns the last non-blank line of events.jsonl, parsed as
// an event. It reads the file from the end in chunks so cost is O(line
// size) rather than O(file size).
func tailLastEvent(path string) (eventType string, ts time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return "", time.Time{}, false
	}

	const chunk int64 = 4096
	var (
		size = st.Size()
		buf  []byte
		off  = size
	)
	for off > 0 {
		read := chunk
		if off < read {
			read = off
		}
		off -= read
		tmp := make([]byte, read)
		if _, err := f.ReadAt(tmp, off); err != nil && err != io.EOF {
			return "", time.Time{}, false
		}
		buf = append(tmp, buf...)
		// Strip any trailing blank lines so we look for the *content* line.
		trimmed := bytes.TrimRight(buf, "\n\r \t")
		if i := bytes.LastIndexByte(trimmed, '\n'); i >= 0 {
			line := strings.TrimSpace(string(trimmed[i+1:]))
			return parseEventLine(line)
		}
		// Whole file (so far) is one line; if we've consumed it all, parse.
		if off == 0 {
			line := strings.TrimSpace(string(trimmed))
			return parseEventLine(line)
		}
	}
	return "", time.Time{}, false
}

func parseEventLine(line string) (string, time.Time, bool) {
	if line == "" {
		return "", time.Time{}, false
	}
	var e eventLine
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		return "", time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	return e.Type, t.UTC(), true
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

// preliminaryState classifies based on the last event type alone.
// Returns (state, needsLsof). When needsLsof is true the caller must
// disambiguate ActiveIdle vs InactiveIdle by checking session.db locking.
func preliminaryState(eventType string) (State, bool) {
	switch {
	case strings.HasPrefix(eventType, "tool.execution_start"),
		strings.HasPrefix(eventType, "agent.processing"):
		return StateWorking, false
	case strings.Contains(eventType, "ask_question"),
		strings.Contains(eventType, "permission_request"):
		return StateWaiting, false
	case eventType == "session.shutdown":
		return StateExited, false
	default:
		return StateUnknown, true
	}
}

