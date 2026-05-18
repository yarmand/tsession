// internal/sessions/statedir.go
package sessions

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/lsofutil"
)

type StateDirInfo struct {
	ID          string
	State       State
	LastEventAt time.Time
	DBLocked    bool
}

type eventLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

func LoadAllStateDirs(root string) ([]StateDirInfo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []StateDirInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		info := StateDirInfo{ID: e.Name()}
		ev, ts, ok := tailLastEvent(filepath.Join(dir, "events.jsonl"))
		if ok {
			info.LastEventAt = ts
			info.State = inferState(ev, filepath.Join(dir, "session.db"), &info.DBLocked)
		} else {
			info.State = StateUnknown
		}
		out = append(out, info)
	}
	return out, nil
}

func tailLastEvent(path string) (eventType string, ts time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer f.Close()

	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			last = line
		}
	}
	if last == "" {
		return "", time.Time{}, false
	}
	var e eventLine
	if err := json.Unmarshal([]byte(last), &e); err != nil {
		return "", time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	return e.Type, t.UTC(), true
}

func inferState(eventType, dbPath string, locked *bool) State {
	switch {
	case strings.HasPrefix(eventType, "tool.execution_start"),
		strings.HasPrefix(eventType, "agent.processing"):
		return StateWorking
	case strings.Contains(eventType, "ask_question"),
		strings.Contains(eventType, "permission_request"):
		return StateWaiting
	case eventType == "session.shutdown":
		return StateExited
	default:
		if lsofutil.IsFileLocked(dbPath) {
			*locked = true
			return StateActiveIdle
		}
		return StateInactiveIdle
	}
}
